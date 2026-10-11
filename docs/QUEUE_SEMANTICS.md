# FamilyBoard Mutation Queue Semantics

FamilyBoard uses an offline-first, local-first architecture for appliance reliability. All calendar writes (create, update, delete) are committed locally to SQLite first, and asynchronous synchronization with upstream calendar providers (Google Calendar, Microsoft Graph, Apple CalDAV) is mediated by the Mutation Queue engine.

## 1. Queue States and State Machine

A mutation item progresses through the following deterministic states:

```
[ pending ] --------> [ processing ] --------> [ completed ]
                             |
                             |-- (transient error / < max retries) --> [ retry ] ---> (scheduled run) ---> [ processing ]
                             |
                             |-- (permanent error / >= max retries) -> [ dead_letter ]
```

* **`pending`**: Mutation has been committed locally and is awaiting dispatch.
* **`processing`**: Mutation is actively being executed against the upstream calendar API.
* **`completed`**: Upstream API confirmed the mutation. Remote IDs/ETags are recorded in the local event.
* **`retry`**: Upstream API failed with a transient error (network timeout, 429 rate limit, 503). Next attempt timestamp is calculated using exponential backoff with jitter.
* **`dead_letter`**: Upstream API failed permanently (400 validation error, 404 missing parent, 422 unprocessable) OR max attempt count (default 5) was exceeded.

## 2. Idempotency Guarantees

Every client-initiated mutation generates a unique `idempotency_key` (e.g. `idem-<timestamp>-<rand>`).
* Provider adapters transmit this idempotency key in client request headers (e.g., `client-request-id` on Microsoft Graph or Google request tracking).
* If an interrupted HTTP request succeeds on the provider side before the connection breaks, replaying the mutation with the same idempotency key will not create a duplicate event.
* Local events store `provider_event_id` and `etag` immediately upon confirmation to prevent subsequent updates from conflicting.

## 3. Ordering and Isolation

1. **Calendar Partitioning**: Mutations are isolated per `calendar_id`.
2. **FIFO Execution**: Pending mutations for a calendar are queried ordered by `created_at ASC`, ensuring that an `update` or `delete` is never applied before the preceding `create`.
3. **In-Flight Claim Serialization**: Mutation processing acquires a mutex lock during batch claim to prevent concurrent workers or fast-path HTTP requests from claiming the same mutation simultaneously.

## 4. Retry Classification & Backoff

Errors are classified using the typed error taxonomy (`internal/errdefs`):

| Error Classification | Examples | Behavior |
|----------------------|----------|----------|
| **Retryable / Unavailable** | DNS timeout, HTTP 429, HTTP 503 | Scheduled for `retry` with exponential backoff + jitter |
| **Authentication** | Token expired, revoked | Attempt token refresh; if revoked, schedule for retry until re-authenticated |
| **Permanent / Validation** | Malformed JSON, 404, invalid duration | **Immediate `dead_letter`** (no futile retries) |

### Backoff Formula

$$\text{delay} = \min(\text{max\_interval}, \text{base\_interval} \times 2^{\text{attempts}}) + \text{jitter}$$

* $\text{base\_interval} = 30\text{s}$ (configurable)
* $\text{max\_interval} = 10\text{m}$
* $\text{jitter} = \text{rand}(0, 1000\text{ms})$

## 5. Poison Messages & Dead-Letter Recovery

* **Poison Message Detection**: A payload that fails JSON unmarshaling or domain validation is flagged as `errdefs.Validation` and transitioned directly to `dead_letter`.
* **Dead-Letter Storage**: Dead-letter items remain in SQLite table `mutation_queue` with their full payload, error description, and final attempt timestamp.
* **Recovery**: An administrator can inspect dead letters in the Admin Health Dashboard and trigger either:
  1. **Replay**: Resets state to `pending` and `attempt_count` to 0.
  2. **Discard**: Removes the dead-letter item if the user chose to abandon the remote sync.
