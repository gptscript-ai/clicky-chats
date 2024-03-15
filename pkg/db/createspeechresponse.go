package db

type CreateSpeechResponse struct {
	JobResponse `json:",inline"`

	Base    `json:",inline"`
	Content []byte `json:"content"`
}

func (*CreateSpeechResponse) IDPrefix() string {
	return "speech-"
}

func (c *CreateSpeechResponse) ToPublic() any {
	return c
}

func (c *CreateSpeechResponse) FromPublic(obj any) error {
	o, ok := obj.(*CreateSpeechResponse)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	*c = *o

	return nil
}
