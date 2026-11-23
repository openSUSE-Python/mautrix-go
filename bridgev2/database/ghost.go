// Copyright (c) 2024 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package database

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/exerrors"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/crypto/canonicaljson"
	"maunium.net/go/mautrix/id"
)

type GhostQuery struct {
	BridgeID networkid.BridgeID
	MetaType MetaTypeCreator
	*dbutil.QueryHelper[*Ghost]
}

type ExtraProfile map[string]json.RawMessage

func (ep *ExtraProfile) Set(key string, value any) error {
	if key == "displayname" || key == "avatar_url" {
		return fmt.Errorf("cannot set reserved profile key %q", key)
	}
	marshaled, err := canonicaljson.Marshal(value)
	if err != nil {
		return err
	}
	if *ep == nil {
		*ep = make(ExtraProfile)
	}
	(*ep)[key] = marshaled
	return nil
}

func (ep *ExtraProfile) With(key string, value any) *ExtraProfile {
	exerrors.PanicIfNotNil(ep.Set(key, value))
	return ep
}

func canonicalize(data json.RawMessage) json.RawMessage {
	canonicalized, _ := canonicaljson.Marshal(data)
	return canonicalized
}

func (ep *ExtraProfile) CopyTo(dest *ExtraProfile) (changed bool) {
	if len(*ep) == 0 {
		return
	}
	if *dest == nil {
		*dest = make(ExtraProfile)
	}
	for key, val := range *ep {
		if key == "displayname" || key == "avatar_url" {
			continue
		}
		existing, exists := (*dest)[key]
		if !exists || !bytes.Equal(canonicalize(existing), val) {
			(*dest)[key] = val
			changed = true
		}
	}
	return
}

// NameQuality ranks how good a ghost's display name is. Ghosts are shared by
// all logins on a bridge, and different logins may know different names for
// the same remote user (for example, one login only knows a phone number while
// another knows the user's profile name). When a network connector ranks the
// names it provides, a name of lower quality will not replace a better one.
//
// The zero value, NameQualityUnranked, means the connector doesn't rank names.
// Unranked names are always accepted, which is the behaviour for connectors
// that don't use name quality at all.
type NameQuality int

const (
	// NameQualityUnranked means the quality of the name is not known.
	NameQualityUnranked NameQuality = 0
	// NameQualityIdentifier is a name built only from identifiers,
	// such as a phone number, username or internal user ID.
	NameQualityIdentifier NameQuality = 100
	// NameQualityProfile is a name the remote user set for themselves,
	// such as a profile name or a business name.
	NameQualityProfile NameQuality = 200
	// NameQualityContact is a name from a contact list.
	NameQualityContact NameQuality = 300
)

type Ghost struct {
	BridgeID networkid.BridgeID
	ID       networkid.UserID

	Name           string
	AvatarID       networkid.AvatarID
	AvatarHash     [32]byte
	AvatarMXC      id.ContentURIString
	NameSet        bool
	NameQuality    NameQuality
	AvatarSet      bool
	ContactInfoSet bool
	IsBot          bool
	Identifiers    []string
	ExtraProfile   ExtraProfile
	Metadata       any
}

const (
	getGhostBaseQuery = `
		SELECT bridge_id, id, name, avatar_id, avatar_hash, avatar_mxc,
		       name_set, name_quality, avatar_set, contact_info_set, is_bot, identifiers, extra_profile, metadata
		FROM ghost
	`
	getGhostByIDQuery       = getGhostBaseQuery + `WHERE bridge_id=$1 AND id=$2`
	getGhostByMetadataQuery = getGhostBaseQuery + `WHERE bridge_id=$1 AND metadata->>$2=$3`
	insertGhostQuery        = `
		INSERT INTO ghost (
			bridge_id, id, name, avatar_id, avatar_hash, avatar_mxc,
			name_set, name_quality, avatar_set, contact_info_set, is_bot, identifiers, extra_profile, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	updateGhostQuery = `
		UPDATE ghost SET name=$3, avatar_id=$4, avatar_hash=$5, avatar_mxc=$6,
		                 name_set=$7, name_quality=$8, avatar_set=$9, contact_info_set=$10, is_bot=$11,
		                 identifiers=$12, extra_profile=$13, metadata=$14
		WHERE bridge_id=$1 AND id=$2
	`
)

func (gq *GhostQuery) GetByID(ctx context.Context, id networkid.UserID) (*Ghost, error) {
	return gq.QueryOne(ctx, getGhostByIDQuery, gq.BridgeID, id)
}

// GetByMetadata returns the ghosts whose metadata field at the given JSON key
// matches the given value.
func (gq *GhostQuery) GetByMetadata(ctx context.Context, key string, value any) ([]*Ghost, error) {
	return gq.QueryMany(ctx, getGhostByMetadataQuery, gq.BridgeID, key, value)
}

func (gq *GhostQuery) Insert(ctx context.Context, ghost *Ghost) error {
	ensureBridgeIDMatches(&ghost.BridgeID, gq.BridgeID)
	return gq.Exec(ctx, insertGhostQuery, ghost.ensureHasMetadata(gq.MetaType).sqlVariables()...)
}

func (gq *GhostQuery) Update(ctx context.Context, ghost *Ghost) error {
	ensureBridgeIDMatches(&ghost.BridgeID, gq.BridgeID)
	return gq.Exec(ctx, updateGhostQuery, ghost.ensureHasMetadata(gq.MetaType).sqlVariables()...)
}

func (g *Ghost) Scan(row dbutil.Scannable) (*Ghost, error) {
	var avatarHash string
	err := row.Scan(
		&g.BridgeID, &g.ID,
		&g.Name, &g.AvatarID, &avatarHash, &g.AvatarMXC,
		&g.NameSet, &g.NameQuality, &g.AvatarSet, &g.ContactInfoSet, &g.IsBot,
		dbutil.JSON{Data: &g.Identifiers}, dbutil.JSON{Data: &g.ExtraProfile}, dbutil.JSON{Data: g.Metadata},
	)
	if err != nil {
		return nil, err
	}
	if avatarHash != "" {
		data, _ := hex.DecodeString(avatarHash)
		if len(data) == 32 {
			g.AvatarHash = *(*[32]byte)(data)
		}
	}
	return g, nil
}

func (g *Ghost) ensureHasMetadata(metaType MetaTypeCreator) *Ghost {
	if g.Metadata == nil {
		g.Metadata = metaType()
	}
	return g
}

func (g *Ghost) sqlVariables() []any {
	var avatarHash string
	if g.AvatarHash != [32]byte{} {
		avatarHash = hex.EncodeToString(g.AvatarHash[:])
	}
	return []any{
		g.BridgeID, g.ID,
		g.Name, g.AvatarID, avatarHash, g.AvatarMXC,
		g.NameSet, g.NameQuality, g.AvatarSet, g.ContactInfoSet, g.IsBot,
		dbutil.JSON{Data: &g.Identifiers}, dbutil.JSON{Data: g.ExtraProfile}, dbutil.JSON{Data: g.Metadata},
	}
}
