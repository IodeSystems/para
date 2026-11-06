package ops

func evidenceMD(ev string) string {
	return "```yaml para-dossier\ntldr: Pick a vendor\n```\n\n## Q\n\n" +
		"```yaml para-question id=q1\ntitle: Which vendor?\noptions: [A, B]\nevidence: " + ev + "\n```\n"
}
