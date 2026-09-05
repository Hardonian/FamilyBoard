package candidates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/dedupe"
	"github.com/Hardonian/FamilyBoard/internal/mail"
	"github.com/Hardonian/FamilyBoard/internal/mail/ics"
	"github.com/Hardonian/FamilyBoard/internal/mail/mime"
	"github.com/Hardonian/FamilyBoard/internal/mail/parser"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/rules"
)

type Pipeline struct {
	store        *db.Store
	mimeParser   *mime.Parser
	nlParser     *parser.NaturalLanguageParser
	aiParser     *parser.LANAIClient
	dedupeEngine *dedupe.Engine
	policyEngine *rules.PolicyEngine
	defaultTZ    string
}

func NewPipeline(
	store *db.Store,
	mimeParser *mime.Parser,
	nlParser *parser.NaturalLanguageParser,
	aiParser *parser.LANAIClient,
	dedupeEngine *dedupe.Engine,
	policyEngine *rules.PolicyEngine,
	defaultTZ string,
) *Pipeline {
	if defaultTZ == "" {
		defaultTZ = "America/Toronto"
	}
	return &Pipeline{
		store:        store,
		mimeParser:   mimeParser,
		nlParser:     nlParser,
		aiParser:     aiParser,
		dedupeEngine: dedupeEngine,
		policyEngine: policyEngine,
		defaultTZ:    defaultTZ,
	}
}

// ProcessRawEmail executes the end-to-end ingestion pipeline on a single email.
func (p *Pipeline) ProcessRawEmail(ctx context.Context, raw mail.RawEmail, mailSourceID string) error {
	// 1. Check if already processed
	alreadyProcessed, err := p.store.IsMessageProcessed(ctx, raw.Provider, raw.ProviderMessageID)
	if err != nil {
		return fmt.Errorf("check message processed: %w", err)
	}
	if alreadyProcessed {
		return nil
	}

	contentHash := dedupe.ComputeContentHash(raw.RawMIME)

	// 2. MIME Parsing
	parsedMIME, err := p.mimeParser.Parse(raw.RawMIME)
	if err != nil {
		slog.Warn("MIME parsing failed for email", "msg_id", raw.ProviderMessageID, "err", err)
		// Record processed with error state to prevent infinite reprocessing loop
		_ = p.store.RecordProcessedMessage(ctx, &models.ProcessedMessage{
			ID:                fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			Provider:          raw.Provider,
			ProviderMessageID: raw.ProviderMessageID,
			MailSourceID:      mailSourceID,
			ReceivedAt:        raw.ReceivedAt,
			ContentHash:       contentHash,
			ProcessingState:   "error",
			LastError:         err.Error(),
		})
		return err
	}

	// 3. Resolve target calendar
	defaultCal, err := p.store.GetDefaultWriteCalendar(ctx)
	targetCalendarID := ""
	if defaultCal != nil {
		targetCalendarID = defaultCal.ID
	}

	var candidates []*models.EventCandidate

	// 4. Check for attached/inline ICS calendar
	if len(parsedMIME.ICSContents) > 0 {
		for _, icsBytes := range parsedMIME.ICSContents {
			cal, err := ics.Parse(icsBytes)
			if err != nil || cal == nil {
				continue
			}
			for _, ev := range cal.Events {
				if ev.Status == "cancelled" {
					continue
				}
				cand := &models.EventCandidate{
					ID:                  fmt.Sprintf("cand-%d", time.Now().UnixNano()),
					SourceProvider:      raw.Provider,
					SourceAccount:       raw.Sender,
					SourceMessageID:     raw.ProviderMessageID,
					SourceSender:        parsedMIME.Sender,
					SourceSubject:       parsedMIME.Subject,
					Title:               ev.Summary,
					Description:         ev.Description,
					Location:            ev.Location,
					StartAt:             ev.StartAt,
					EndAt:               ev.EndAt,
					Timezone:            ev.Timezone,
					AllDay:              ev.AllDay,
					Confidence:          0.98, // High confidence for structured RFC 5545
					ParserMethod:        "ics",
					SuggestedCalendarID: targetCalendarID,
					State:               models.CandidatePending,
					CreatedAt:           time.Now(),
				}
				candidates = append(candidates, cand)
			}
		}
	}

	// 5. If no ICS events found, run deterministic natural language parser
	if len(candidates) == 0 && parsedMIME.TextBody != "" {
		parsedEvent, err := p.nlParser.Parse(parsedMIME.TextBody, parsedMIME.Subject, raw.ReceivedAt)
		if err == nil && parsedEvent != nil && parsedEvent.Confidence >= 0.40 {
			ambiguityReason := ""
			if len(parsedEvent.AmbiguityReasons) > 0 {
				ambiguityReason = strings.Join(parsedEvent.AmbiguityReasons, "; ")
			}
			cand := &models.EventCandidate{
				ID:                  fmt.Sprintf("cand-%d", time.Now().UnixNano()),
				SourceProvider:      raw.Provider,
				SourceAccount:       raw.Sender,
				SourceMessageID:     raw.ProviderMessageID,
				SourceSender:        parsedMIME.Sender,
				SourceSubject:       parsedMIME.Subject,
				Title:               parsedEvent.Title,
				Description:         parsedEvent.Description,
				Location:            parsedEvent.Location,
				StartAt:             parsedEvent.StartAt,
				EndAt:               parsedEvent.EndAt,
				Timezone:            parsedEvent.Timezone,
				AllDay:              parsedEvent.AllDay,
				Confidence:          parsedEvent.Confidence,
				ParserMethod:        "deterministic_nl",
				AmbiguityReason:     ambiguityReason,
				SuggestedCalendarID: targetCalendarID,
				State:               models.CandidatePending,
				CreatedAt:           time.Now(),
			}
			candidates = append(candidates, cand)
		} else if p.aiParser != nil {
			// 6. Optional LAN AI Fallback (if enabled)
			aiEvent, err := p.aiParser.Parse(ctx, parsedMIME.TextBody, p.defaultTZ)
			if err == nil && aiEvent != nil && aiEvent.Confidence >= 0.60 {
				cand := &models.EventCandidate{
					ID:                  fmt.Sprintf("cand-%d", time.Now().UnixNano()),
					SourceProvider:      raw.Provider,
					SourceAccount:       raw.Sender,
					SourceMessageID:     raw.ProviderMessageID,
					SourceSender:        parsedMIME.Sender,
					SourceSubject:       parsedMIME.Subject,
					Title:               aiEvent.Title,
					Location:            aiEvent.Location,
					StartAt:             aiEvent.StartAt,
					EndAt:               aiEvent.EndAt,
					Timezone:            aiEvent.Timezone,
					AllDay:              aiEvent.AllDay,
					Confidence:          aiEvent.Confidence,
					ParserMethod:        "lan_llm",
					SuggestedCalendarID: targetCalendarID,
					State:               models.CandidatePending,
					CreatedAt:           time.Now(),
				}
				candidates = append(candidates, cand)
			}
		}
	}

	if len(candidates) == 0 {
		// Ignored email (not a calendar event)
		_ = p.store.RecordProcessedMessage(ctx, &models.ProcessedMessage{
			ID:                fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			Provider:          raw.Provider,
			ProviderMessageID: raw.ProviderMessageID,
			MailSourceID:      mailSourceID,
			ReceivedAt:        raw.ReceivedAt,
			ContentHash:       contentHash,
			ProcessingState:   "ignored",
			ParserVersion:     "1.0",
		})
		return nil
	}

	// 7. Check Sender Trust & Deduplication for each candidate
	isTrustedSender, _ := p.policyEngine.IsSenderTrusted(ctx, parsedMIME.Sender)

	for _, cand := range candidates {
		dedupeResult, _ := p.dedupeEngine.CheckEventDuplicate(ctx, cand.SuggestedCalendarID, "", cand.Title, cand.StartAt)
		isDuplicate := (dedupeResult != nil && dedupeResult.IsDuplicate)

		// 8. Policy Evaluation
		finalState, policyReason := p.policyEngine.Evaluate(cand, isTrustedSender, isDuplicate)
		cand.State = finalState

		if isDuplicate {
			cand.AmbiguityReason = dedupeResult.Reason
		} else if policyReason != "" && finalState == models.CandidatePending {
			if cand.AmbiguityReason != "" {
				cand.AmbiguityReason += "; " + policyReason
			} else {
				cand.AmbiguityReason = policyReason
			}
		}

		if err := p.store.CreateEventCandidate(ctx, cand); err != nil {
			slog.Warn("Failed to persist event candidate", "cand_id", cand.ID, "err", err)
			continue
		}

		var createdEventID *string

		// 9. If Auto-Approved -> Create Local Event & Enqueue Outbound Mutation!
		if finalState == models.CandidateAutoApproved && cand.SuggestedCalendarID != "" {
			eventID := fmt.Sprintf("evt-local-%d", time.Now().UnixNano())
			createdEventID = &eventID
			now := time.Now()

			localEvent := &models.Event{
				ID:             eventID,
				Provider:       "google",
				CalendarID:     cand.SuggestedCalendarID,
				Title:          cand.Title,
				Description:    cand.Description,
				Location:       cand.Location,
				StartAt:        cand.StartAt,
				EndAt:          cand.EndAt,
				Timezone:       cand.Timezone,
				AllDay:         cand.AllDay,
				Status:         "confirmed",
				Source:         "email_candidate",
				LocalCreatedAt: now,
				LocalUpdatedAt: now,
			}

			if err := p.store.UpsertEvent(ctx, localEvent); err == nil {
				eventJSON, _ := json.Marshal(localEvent)
				mutation := &models.MutationQueueItem{
					ID:             fmt.Sprintf("mut-%d", time.Now().UnixNano()),
					Operation:      "create",
					Provider:       "google",
					CalendarID:     cand.SuggestedCalendarID,
					LocalEventID:   eventID,
					Payload:        string(eventJSON),
					IdempotencyKey: fmt.Sprintf("idemp-mail-%s", cand.ID),
					AttemptCount:   0,
					NextAttemptAt:  now,
					State:          models.QueuePending,
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				_ = p.store.EnqueueMutation(ctx, mutation)
			}
		}

		// Record processed message
		candID := cand.ID
		_ = p.store.RecordProcessedMessage(ctx, &models.ProcessedMessage{
			ID:                fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			Provider:          raw.Provider,
			ProviderMessageID: raw.ProviderMessageID,
			MailSourceID:      mailSourceID,
			ReceivedAt:        raw.ReceivedAt,
			ContentHash:       contentHash,
			ProcessingState:   "processed",
			ParserVersion:     "1.0",
			CandidateID:       &candID,
			EventID:           createdEventID,
		})
	}

	return nil
}
