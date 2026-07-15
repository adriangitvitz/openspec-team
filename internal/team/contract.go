package team

import "encoding/json"

// ContractTool describes one advertised tool and when it appears.
type ContractTool struct {
	Name         string          `json:"name"`
	Precondition string          `json:"precondition"`
	Schema       json.RawMessage `json:"schema"`
}

// Contract is the runner's integration surface for external harnesses.
type Contract struct {
	Tools         []ContractTool `json:"tools"`
	NeedsProtocol struct {
		ExitCode       int          `json:"exitCode"`
		PayloadExample NeedsPayload `json:"payloadExample"`
		PayloadNotes   string       `json:"payloadNotes"`
		NeedsFile      string       `json:"needsFile"`
		Fulfillment    []string     `json:"fulfillment"`
		RoundTripCap   int          `json:"roundTripCap"`
		CapOverride    string       `json:"capOverride"`
	} `json:"needsProtocol"`
	ExtractionSiblings   string `json:"extractionSiblings"`
	ConfidentialBoundary string `json:"confidentialBoundary"`
}

// IntegrationContract returns the static, deterministic contract printed by `openspec team tools`.
func IntegrationContract() Contract {
	var c Contract
	always := "always advertised"
	search := "advertised when team.search.mcp_url is configured in openspec/config.yaml"

	var all []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	json.Unmarshal(searchToolSchemas, &all)
	var raw []json.RawMessage
	json.Unmarshal(searchToolSchemas, &raw)
	for i, def := range all {
		pre := always
		if def.Function.Name == "web_search" || def.Function.Name == "fetch_page" {
			pre = search
		}
		c.Tools = append(c.Tools, ContractTool{Name: def.Function.Name, Precondition: pre, Schema: raw[i]})
	}

	c.NeedsProtocol.ExitCode = NeedsExitCode
	c.NeedsProtocol.PayloadExample = NeedsPayload{
		ChangeName: "example-change",
		Persona:    "product-owner",
		Artifact:   "research",
		RoundTrip:  1,
		Requests: []ExtractionRequest{{
			Path:      "docs/spec.pdf",
			Detail:    "field table in section 4.2, pages 12-15",
			Rationale: "the current extraction omits the exact field lengths",
		}},
	}
	c.NeedsProtocol.PayloadNotes = "always printed as JSON on stdout when the run pauses; human rendering goes to stderr"
	c.NeedsProtocol.NeedsFile = NeedsFileName + " in the change directory; pending requests are cleared by the next successful run of the same persona and artifact, round-trip counts are retained; the file is removed when nothing remains"
	c.NeedsProtocol.Fulfillment = []string{
		"read the needs payload (stdout or the needs file)",
		"for each request, extract the asked detail from the source document and write or refine its sibling extraction <name>.<ext>.md with a provenance header (source path, source-sha256 content hash, source-modified time, extraction date, section/page markers)",
		"re-run the same `openspec team run` invocation; the evidence bundle now carries the extraction",
		"if the harness cannot parse the document, surface the request to a human instead of fabricating an extraction",
	}
	c.NeedsProtocol.RoundTripCap = DefaultMaxExtractionRoundTrips
	c.NeedsProtocol.CapOverride = "--max-extraction-roundtrips"

	c.ExtractionSiblings = "binary documents (.pdf, .docx, .xlsx, .pptx) are never inlined raw and read_file refuses them: the bundle inlines the sibling extraction <name>.<ext>.md when present (stale-checked by comparing the provenance source-sha256 against the source's current content) and lists documents without one under 'needs extraction'"
	c.ConfidentialBoundary = "team.confidential in openspec/config.yaml lists root-relative glob patterns (`*` within a path segment, `**` across segments) naming files withheld from external runners; sibling extractions inherit their source's confidentiality. For personas on an external runner the evidence bundle lists matches under 'withheld' (path visible, content absent), read_file refuses them, grep silently skips them, list_dir still shows names (existence is not a secret), and request_extraction on a confidential source is an in-run error — never a pause — instructing the model to ask the human at the gate for a curated release. Pattern-match failures fail closed (treated as confidential). Harnesses must never auto-fulfill an extraction of a confidential source for an external persona; releasing content is a deliberate human act via an explicitly sanitized copy saved outside the confidential set"
	return c
}
