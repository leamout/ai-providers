package gemini

import "encoding/json"

type realtimeClientMessage struct {
	Setup         *realtimeSetup        `json:"setup,omitempty"`
	RealtimeInput *realtimeInput        `json:"realtimeInput,omitempty"`
	ToolResponse  *realtimeToolResponse `json:"toolResponse,omitempty"`
}

type realtimeSetup struct {
	Model                    string                   `json:"model"`
	GenerationConfig         realtimeGenerationConfig `json:"generationConfig"`
	SystemInstruction        *realtimeContent         `json:"systemInstruction,omitempty"`
	Tools                    []realtimeTool           `json:"tools,omitempty"`
	InputAudioTranscription  map[string]any           `json:"inputAudioTranscription,omitempty"`
	OutputAudioTranscription map[string]any           `json:"outputAudioTranscription,omitempty"`
}

type realtimeGenerationConfig struct {
	ResponseModalities []string              `json:"responseModalities"`
	SpeechConfig       *realtimeSpeechConfig `json:"speechConfig,omitempty"`
}

type realtimeSpeechConfig struct {
	VoiceConfig realtimeVoiceConfig `json:"voiceConfig"`
}

type realtimeVoiceConfig struct {
	PrebuiltVoiceConfig realtimePrebuiltVoiceConfig `json:"prebuiltVoiceConfig"`
}

type realtimePrebuiltVoiceConfig struct {
	VoiceName string `json:"voiceName"`
}

type realtimeContent struct {
	Role  string         `json:"role,omitempty"`
	Parts []realtimePart `json:"parts"`
}

type realtimePart struct {
	Text       string        `json:"text,omitempty"`
	InlineData *realtimeBlob `json:"inlineData,omitempty"`
}

type realtimeBlob struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType,omitempty"`
}

type realtimeTool struct {
	FunctionDeclarations []realtimeFunctionDeclaration `json:"functionDeclarations"`
}

type realtimeFunctionDeclaration struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type realtimeInput struct {
	Audio *realtimeBlob `json:"audio,omitempty"`
}

type realtimeToolResponse struct {
	FunctionResponses []realtimeFunctionResponse `json:"functionResponses"`
}

type realtimeFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type realtimeServerMessage struct {
	SetupComplete *struct{}              `json:"setupComplete,omitempty"`
	ServerContent *realtimeServerContent `json:"serverContent,omitempty"`
	ToolCall      *realtimeToolCall      `json:"toolCall,omitempty"`
	UsageMetadata *realtimeUsageMetadata `json:"usageMetadata,omitempty"`
	GoAway        *realtimeGoAway        `json:"goAway,omitempty"`
}

type realtimeServerContent struct {
	ModelTurn                 *realtimeContent       `json:"modelTurn,omitempty"`
	GenerationComplete        bool                   `json:"generationComplete,omitempty"`
	TurnComplete              bool                   `json:"turnComplete,omitempty"`
	Interrupted               bool                   `json:"interrupted,omitempty"`
	InterimInputTranscription *realtimeTranscription `json:"interimInputTranscription,omitempty"`
	InputTranscription        *realtimeTranscription `json:"inputTranscription,omitempty"`
	OutputTranscription       *realtimeTranscription `json:"outputTranscription,omitempty"`
}

type realtimeTranscription struct {
	Text string `json:"text"`
}

type realtimeToolCall struct {
	FunctionCalls []realtimeFunctionCall `json:"functionCalls"`
}

type realtimeFunctionCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type realtimeUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type realtimeGoAway struct {
	TimeLeft string `json:"timeLeft"`
}
