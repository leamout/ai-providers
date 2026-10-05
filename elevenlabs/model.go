package elevenlabs

type inputMessage struct {
	Text                 string `json:"text"`
	TryTriggerGeneration bool   `json:"try_trigger_generation,omitempty"`
}

type outputMessage struct {
	Audio       string `json:"audio"`
	Final       bool   `json:"is_final"`
	LegacyFinal bool   `json:"isFinal"`
	Error       string `json:"error"`
	Message     string `json:"message"`
}
