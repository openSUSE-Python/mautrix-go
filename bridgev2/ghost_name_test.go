// Copyright (c) 2026 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bridgev2

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"maunium.net/go/mautrix/bridgev2/database"
)

func TestGhostPrepareNameQuality(t *testing.T) {
	const (
		unranked   = database.NameQualityUnranked
		identifier = database.NameQualityIdentifier
		profile    = database.NameQualityProfile
		contact    = database.NameQualityContact
	)
	tests := []struct {
		name           string
		currentName    string
		currentQuality database.NameQuality
		newName        string
		newQuality     database.NameQuality
		wantName       string
		wantQuality    database.NameQuality
		wantChanged    bool
		wantQualityChg bool
		currentUnset   bool
	}{
		{"empty ghost accepts identifier", "", unranked, "+123", identifier, "+123", identifier, true, true, false},
		{"profile replaces identifier", "+123", identifier, "Alice", profile, "Alice", profile, true, true, false},
		{"identifier doesn't replace profile", "Alice", profile, "+123", identifier, "Alice", profile, false, false, false},
		{"profile doesn't replace contact", "Ali", contact, "Alice", profile, "Ali", contact, false, false, false},
		{"equal quality replaces", "Alice", profile, "Alicia", profile, "Alicia", profile, true, false, false},
		{"unranked always replaces", "Alice", contact, "+123", unranked, "+123", unranked, true, true, false},
		{"ranked replaces unranked", "+123", unranked, "+123", identifier, "+123", identifier, false, true, false},
		{"same name keeps better quality", "Alice", contact, "Alice", profile, "Alice", contact, false, false, false},
		{"same name raises quality", "Alice", profile, "Alice", contact, "Alice", contact, false, true, false},
		{"rejected name retries unsent current name", "Alice", profile, "+123", identifier, "Alice", profile, true, false, true},
		{"same name retries unsent name", "Alice", profile, "Alice", profile, "Alice", profile, true, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ghost := &Ghost{Ghost: &database.Ghost{
				Name:        tt.currentName,
				NameQuality: tt.currentQuality,
				NameSet:     tt.currentName != "" && !tt.currentUnset,
			}}
			changed, qualityChanged := ghost.prepareName(tt.newName, tt.newQuality)
			assert.Equal(t, tt.wantChanged, changed, "changed")
			assert.Equal(t, tt.wantQualityChg, qualityChanged, "qualityChanged")
			assert.Equal(t, tt.wantName, ghost.Name)
			assert.Equal(t, tt.wantQuality, ghost.NameQuality)
			if changed {
				assert.False(t, ghost.NameSet, "NameSet must be false when the name has to be pushed")
			}
		})
	}
}

func TestGhostUpdateNameQualityOnly(t *testing.T) {
	ghost := &Ghost{Ghost: &database.Ghost{
		Name:        "Alice",
		NameQuality: database.NameQualityProfile,
		NameSet:     true,
	}}
	// Same name, so nothing is sent to Matrix (Intent is nil), but the quality reset must be reported
	assert.True(t, ghost.UpdateName(t.Context(), "Alice"))
	assert.Equal(t, database.NameQualityUnranked, ghost.NameQuality)
	assert.False(t, ghost.UpdateName(t.Context(), "Alice"))
}
