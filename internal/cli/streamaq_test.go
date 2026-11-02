package cli

// streamAQParityCases are the parity rows of stream AQ: the friction report.
func streamAQParityCases() map[string]parityCase {
	return map[string]parityCase{
		"report_friction": {`{"by":"reason"}`, []string{"report", "friction", "--by", "reason"}, true},
	}
}
