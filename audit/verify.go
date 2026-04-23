package audit

import (
	"context"
	"encoding/json"
	"fmt"
)

// VerificationResult is the outcome of a chain-integrity check.
type VerificationResult struct {
	Valid          bool
	FirstInvalidID int64  // 0 if Valid
	Reason         string // human-readable explanation when not Valid
	EntriesChecked int
}

// Verify walks the entire audit chain in insertion order, recomputing each
// entry's hash and verifying that the prev_hash links are intact.
//
// A forged or corrupted entry is detected the moment its recomputed hash
// diverges from the stored entry_hash, or its prev_hash does not match the
// preceding entry.
func Verify(ctx context.Context, logger Logger) (*VerificationResult, error) {
	const batchSize = 200
	result := &VerificationResult{}
	prevHash := GenesisHash

	for offset := 0; ; offset += batchSize {
		entries, err := logger.Query(ctx, Filter{
			Limit:     batchSize,
			Offset:    offset,
			Ascending: true,
		})
		if err != nil {
			return nil, fmt.Errorf("audit verify: querying entries: %w", err)
		}
		if len(entries) == 0 {
			break
		}

		for _, e := range entries {
			result.EntriesChecked++

			// Verify the chain link.
			if e.PrevHash != prevHash {
				result.Valid = false
				result.FirstInvalidID = e.ID
				result.Reason = fmt.Sprintf(
					"entry %d: prev_hash mismatch (expected %.16s…, got %.16s…)",
					e.ID, prevHash, e.PrevHash)
				return result, nil
			}

			// Recompute and verify the entry hash.
			// Re-serialise via json.Marshal to get the same canonical JSON the
			// writer used (Go's json.Marshal sorts map keys deterministically).
			metaJSON, err := json.Marshal(e.Metadata)
			if err != nil || len(metaJSON) == 0 {
				metaJSON = []byte("{}")
			}
			expected := ComputeEntryHash(e, metaJSON)
			if e.EntryHash != expected {
				result.Valid = false
				result.FirstInvalidID = e.ID
				result.Reason = fmt.Sprintf(
					"entry %d: entry_hash mismatch (stored %.16s…, computed %.16s…)",
					e.ID, e.EntryHash, expected)
				return result, nil
			}

			prevHash = e.EntryHash
		}
	}

	result.Valid = true
	return result, nil
}
