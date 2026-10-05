package assemblyai

type event struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Transcript string `json:"transcript"`
	End        bool   `json:"end_of_turn"`
	Formatted  bool   `json:"turn_is_formatted"`
	Error      string `json:"error"`
}

type controlMessage struct {
	Type string `json:"type"`
}
