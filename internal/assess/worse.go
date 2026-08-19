package assess

// Worse returns the higher-risk of two verdicts. This function is the
// entire security argument for letting a model participate: the model's
// verdict is only ever combined through Worse, so a model -- or anything
// that has influenced one, including text inside the diff it was shown --
// can narrow a lane and can never widen it. There is no other path, no
// override flag, and no "trust the model when it is confident".
func Worse(a, b Risk) Risk {
	if riskRank(b) > riskRank(a) {
		return b
	}
	return a
}
