package azure

type recognitionResult struct {
	Text              string `json:"Text"`
	DisplayText       string `json:"DisplayText"`
	RecognitionStatus string `json:"RecognitionStatus"`
}
