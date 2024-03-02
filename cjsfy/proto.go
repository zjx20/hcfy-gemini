package cjsfy

type Content struct {
	Role  string  `json:"role"`
	Parts []*Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GeminiAPIRequest struct {
	Contents []*Content `json:"contents"`
}

type GeminiAPIResponse struct {
	Candidates []*Candidate `json:"candidates"`
}

type Candidate struct {
	Content *Content `json:"content"`
}
