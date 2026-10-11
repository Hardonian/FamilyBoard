package calendar

import "sort"

// Capabilities explicitly describes what a provider/account type can do, so the
// UI, queue, and sync engine never have to infer behaviour from provider names.
type Capabilities struct {
	Read            bool `json:"read"`
	Write           bool `json:"write"`
	Recurrence      bool `json:"recurrence"`
	Attendees       bool `json:"attendees"`
	Meetings        bool `json:"meetings"`
	Reminders       bool `json:"reminders"`
	IncrementalSync bool `json:"incremental_sync"`
}

var providerCapabilities = map[string]Capabilities{
	"google": {
		Read: true, Write: true, Recurrence: true, Attendees: true,
		Meetings: true, Reminders: true, IncrementalSync: true,
	},
	"microsoft": {
		Read: true, Write: true, Recurrence: true, Attendees: true,
		Meetings: true, Reminders: true, IncrementalSync: true,
	},
	// Apple CalDAV supports two-way sync via ctag/etag polling but not
	// incremental tokens; webcal subscriptions are read-only (see ForAccount).
	"apple": {
		Read: true, Write: true, Recurrence: true, Attendees: false,
		Meetings: false, Reminders: false, IncrementalSync: false,
	},
	// Local household calendar lives only on the appliance.
	"local": {
		Read: true, Write: true, Recurrence: true,
	},
}

// CapabilitiesFor returns the capability set for a provider. Unknown providers
// get the zero value (no capabilities) so new integrations fail closed.
func CapabilitiesFor(provider string) Capabilities {
	return providerCapabilities[provider]
}

// ForAccount refines provider capabilities using account-level facts. A webcal
// subscription is read-only regardless of provider defaults.
func ForAccount(provider string, readOnlySubscription bool) Capabilities {
	c := CapabilitiesFor(provider)
	if readOnlySubscription {
		c.Write = false
		c.IncrementalSync = false
	}
	return c
}

// KnownProviders lists providers that have a declared capability set.
func KnownProviders() []string {
	names := make([]string, 0, len(providerCapabilities))
	for name := range providerCapabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
