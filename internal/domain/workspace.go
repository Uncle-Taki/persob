package domain

import "github.com/google/uuid"

// WorkspaceID is fixed for this single-installation, single-gateway release.
// Buyer identity is carried by grants/interests/requests, never this ID.
var WorkspaceID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
