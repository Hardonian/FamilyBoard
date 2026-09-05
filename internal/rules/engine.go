package rules

import (
	"context"
	"strings"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

type PolicyEngine struct {
	store               *db.Store
	confidenceThreshold float64
}

func NewPolicyEngine(store *db.Store, confidenceThreshold float64) *PolicyEngine {
	if confidenceThreshold <= 0 {
		confidenceThreshold = 0.90
	}
	return &PolicyEngine{
		store:               store,
		confidenceThreshold: confidenceThreshold,
	}
}

// IsSenderTrusted checks whether the sender email or domain is approved in trusted_senders or belongs to a household member.
func (p *PolicyEngine) IsSenderTrusted(ctx context.Context, senderEmail string) (bool, error) {
	senderEmail = strings.ToLower(strings.TrimSpace(senderEmail))
	if senderEmail == "" {
		return false, nil
	}

	if p.store != nil {
		// Household members with registered emails are inherently trusted
		people, err := p.store.ListPeople(ctx)
		if err == nil {
			for _, person := range people {
				if person.Enabled && person.Email != "" && strings.EqualFold(person.Email, senderEmail) {
					return true, nil
				}
			}
		}

		senders, err := p.store.ListTrustedSenders(ctx)
		if err != nil {
			return false, err
		}

		for _, s := range senders {
			if !s.Enabled {
				continue
			}
			if s.Email != "" && strings.EqualFold(s.Email, senderEmail) {
				return true, nil
			}
			if s.Domain != "" && strings.HasSuffix(senderEmail, "@"+strings.ToLower(s.Domain)) {
				return true, nil
			}
		}
	}

	return false, nil
}

// Evaluate determines whether an event candidate is auto-approved or requires human review.
func (p *PolicyEngine) Evaluate(candidate *models.EventCandidate, isTrustedSender, isDuplicate bool) (models.CandidateState, string) {
	if isDuplicate {
		return models.CandidateDuplicate, "duplicate event detected"
	}

	if !isTrustedSender {
		return models.CandidatePending, "sender not in trusted allowlist"
	}

	if candidate.Confidence < p.confidenceThreshold {
		return models.CandidatePending, "confidence below auto-approval threshold"
	}

	if candidate.AmbiguityReason != "" {
		return models.CandidatePending, "ambiguity identified during parsing: " + candidate.AmbiguityReason
	}

	if candidate.SuggestedCalendarID == "" {
		return models.CandidatePending, "no destination calendar resolved"
	}

	if candidate.StartAt.IsZero() {
		return models.CandidatePending, "start date/time is missing"
	}

	return models.CandidateAutoApproved, "passed all auto-approval policy requirements"
}
