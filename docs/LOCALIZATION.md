# Localization Architecture

`server.locale` accepts a BCP 47 language tag and defaults to `en-CA`. The server places it on the document `<html lang>` attribute, JavaScript date/number formatting uses the browser's matching `Intl` locale, and server-rendered shared navigation uses keyed catalogs in `internal/i18n`.

Catalog lookup falls back from the full tag (`fr-CA`) to its language (`fr`) and then English. Unknown keys render as the key so missing translations are visible in review. English and French Canadian navigation catalogs establish the supported extension pattern; new server strings should use `t "catalog.key"`, while dynamic dates must continue using locale-aware formatting rather than hard-coded numeric formats.

To add a locale:

1. Add its language catalog in `internal/i18n/catalog.go` and tests for fallback behavior.
2. Replace newly touched literal UI labels with stable semantic keys.
3. Run Go template tests and the Playwright desktop/mobile accessibility suite with `server.locale` set to the new locale.
4. Check long-label wrapping, pluralization, date order, 12/24-hour behavior, screen-reader language, and right-to-left layout if applicable.

Event titles, descriptions, provider names, and household-entered values are user data and are never machine-translated.
