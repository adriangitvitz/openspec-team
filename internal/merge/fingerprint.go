package merge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adriangitvitz/openspec-team/internal/fsutil"
	"github.com/adriangitvitz/openspec-team/internal/parser"
)

// Fingerprints hash base requirements at capture time; a mismatch at archive blocks instead of silently clobbering.

// MetaFilename is the machine-owned change metadata file (fingerprints).
const MetaFilename = "meta.json"

// AbsentFingerprint marks a requirement missing from the base at capture time.
const AbsentFingerprint = "absent"

// CapabilityFingerprints holds the base-requirement hashes for one capability.
type CapabilityFingerprints struct {
	CapturedAt   string            `json:"capturedAt"`
	Requirements map[string]string `json:"requirements"`
}

// Meta is the meta.json document.
type Meta struct {
	Version      int                               `json:"version"`
	Fingerprints map[string]CapabilityFingerprints `json:"fingerprints"`
}

// HashRequirement is sha256 of the raw block with LF endings, right-trimmed.
func HashRequirement(raw string) string {
	normalized := strings.TrimRight(parser.NormalizeLineEndings(raw), " \t\n")
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// LoadMeta reads a change's meta.json, or an empty Meta if absent.
func LoadMeta(changeDir string) (*Meta, error) {
	content, err := os.ReadFile(filepath.Join(changeDir, MetaFilename))
	if os.IsNotExist(err) {
		return &Meta{Version: 1, Fingerprints: map[string]CapabilityFingerprints{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(content, &meta); err != nil {
		return nil, err
	}
	if meta.Fingerprints == nil {
		meta.Fingerprints = map[string]CapabilityFingerprints{}
	}
	return &meta, nil
}

// SaveMeta writes meta.json atomically.
func SaveMeta(changeDir string, meta *Meta) error {
	content, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(changeDir, MetaFilename), append(content, '\n'), 0o644)
}

func referencedBaseNames(plan parser.DeltaPlan) []string {
	var names []string
	for _, block := range plan.Modified {
		names = append(names, parser.NormalizeRequirementName(block.Name))
	}
	for _, name := range plan.Removed {
		names = append(names, parser.NormalizeRequirementName(name))
	}
	for _, ren := range plan.Renamed {
		names = append(names, parser.NormalizeRequirementName(ren.From))
	}
	return names
}

func baseRequirementHashes(targetSpecPath string, names []string) map[string]string {
	hashes := map[string]string{}
	blocks := map[string]string{}
	if content, err := os.ReadFile(targetSpecPath); err == nil {
		for _, block := range parser.ExtractRequirementsSection(string(content)).Blocks {
			blocks[parser.NormalizeRequirementName(block.Name)] = block.Raw
		}
	}
	for _, name := range names {
		if raw, ok := blocks[name]; ok {
			hashes[name] = HashRequirement(raw)
		} else {
			hashes[name] = AbsentFingerprint
		}
	}
	return hashes
}

// CaptureFingerprints records base hashes for every requirement the deltas
// reference. First-touch wins; only refresh overwrites existing entries.
func CaptureFingerprints(changeDir, mainSpecsDir string, refresh bool) error {
	updates, err := FindSpecUpdates(changeDir, mainSpecsDir)
	if err != nil || len(updates) == 0 {
		return err
	}
	meta, err := LoadMeta(changeDir)
	if err != nil {
		return err
	}

	changed := false
	for _, update := range updates {
		content, err := os.ReadFile(update.Source)
		if err != nil {
			continue
		}
		names := referencedBaseNames(parser.ParseDeltaSpec(string(content)))
		if len(names) == 0 {
			continue
		}
		capability := filepath.Base(filepath.Dir(update.Target))
		entry, exists := meta.Fingerprints[capability]
		if refresh || !exists {
			entry = CapabilityFingerprints{Requirements: map[string]string{}}
		}
		if entry.Requirements == nil {
			entry.Requirements = map[string]string{}
		}

		current := baseRequirementHashes(update.Target, names)
		for _, name := range names {
			if _, seen := entry.Requirements[name]; seen && !refresh {
				continue
			}
			entry.Requirements[name] = current[name]
			changed = true
		}
		if changed {
			entry.CapturedAt = time.Now().UTC().Format(time.RFC3339)
		}
		meta.Fingerprints[capability] = entry
	}

	if !changed {
		return nil
	}
	return SaveMeta(changeDir, meta)
}

// Conflict describes a requirement whose base changed after capture.
type Conflict struct {
	Capability  string `json:"capability"`
	Requirement string `json:"requirement"`
}

// VerifyFingerprints compares recorded base hashes with the current base
// spec. checked is false when no fingerprints existed to compare.
func VerifyFingerprints(changeDir string, update SpecUpdate, plan parser.DeltaPlan) (conflicts []Conflict, checked bool, err error) {
	names := referencedBaseNames(plan)
	if len(names) == 0 {

		return nil, true, nil
	}

	meta, err := LoadMeta(changeDir)
	if err != nil {
		return nil, false, err
	}
	capability := filepath.Base(filepath.Dir(update.Target))
	entry, ok := meta.Fingerprints[capability]
	if !ok || len(entry.Requirements) == 0 {
		return nil, false, nil
	}
	current := baseRequirementHashes(update.Target, names)

	modifiedByName := map[string]string{}
	for _, block := range plan.Modified {
		modifiedByName[parser.NormalizeRequirementName(block.Name)] = block.Raw
	}

	for _, name := range names {
		recorded, ok := entry.Requirements[name]
		if !ok {
			continue
		}
		if recorded == current[name] {
			continue
		}
		if raw, ok := modifiedByName[name]; ok && HashRequirement(raw) == current[name] {
			continue
		}
		conflicts = append(conflicts, Conflict{Capability: capability, Requirement: name})
	}
	return conflicts, true, nil
}

// ConflictError formats fingerprint conflicts with remediation guidance.
type ConflictError struct {
	ChangeName string
	Conflicts  []Conflict
}

func (e *ConflictError) Error() string {
	var b strings.Builder
	b.WriteString("archive blocked: the base spec changed since this change captured it (likely another change was archived):\n")
	for _, c := range e.Conflicts {
		b.WriteString("  - " + c.Capability + ": \"" + c.Requirement + "\"\n")
	}
	b.WriteString("Update the delta spec(s) from openspec/specs/<capability>/spec.md, then run: openspec validate " + e.ChangeName + " --refresh-fingerprints")
	return b.String()
}
