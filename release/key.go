// Package release holds what every para build needs to install a release:
// the minisign public key that release manifests are signed with, and where
// the newest manifest is.
package release

import _ "embed"

// PublicKey is the minisign public key file (one key line, or several during
// a rotation). The key is dun's (ID 2CC27A171DE03D42), shared by decision
// (2026-11-10): para's manifests name para in their product field and their
// signed trusted comment, so a dun manifest never verifies as para.
//
//go:embed minisign.pub
var PublicKey string

// ManifestURL is the newest release's manifest; its signature is beside it,
// ManifestURL + ".minisig".
const ManifestURL = "https://github.com/IodeSystems/para/releases/latest/download/para-manifest.json"
