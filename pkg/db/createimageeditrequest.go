package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	oapitypes "github.com/oapi-codegen/runtime/types"
)

type CreateImageEditRequest struct {
	JobRequest `json:",inline"`

	Image          []byte  `json:"image"`
	Mask           []byte  `json:"mask"`
	Model          *string `json:"model"`
	N              *int    `json:"n"`
	Prompt         string  `json:"prompt"`
	ResponseFormat *string `json:"response_format"`
	Size           *string `json:"size"`
	User           *string `json:"user,omitempty"`
}

func (*CreateImageEditRequest) IDPrefix() string {
	return "imageedit-"
}

func (c *CreateImageEditRequest) ToPublic() any {
	if c == nil {
		return nil
	}

	model := new(openai.CreateImageEditRequest_Model)
	if err := model.FromCreateImageEditRequestModel0(z.Dereference(c.Model)); err != nil {
		return nil
	}

	image := new(oapitypes.File)
	if len(c.Image) > 0 {
		image.InitFromBytes(c.Image, "image")
	}

	var mask *oapitypes.File
	if len(c.Mask) > 0 {
		mask = new(oapitypes.File)
		mask.InitFromBytes(c.Mask, "mask")
	}

	//nolint:govet
	return &openai.CreateImageEditRequest{
		*image,
		mask,
		model,
		c.N,
		c.Prompt,
		(*openai.CreateImageEditRequestResponseFormat)(c.ResponseFormat),
		(*openai.CreateImageEditRequestSize)(c.Size),
		c.User,
	}
}

func (c *CreateImageEditRequest) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateImageEditRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || c == nil {
		return nil
	}

	var model *string
	if o.Model != nil {
		m, err := o.Model.AsCreateImageEditRequestModel0()
		if err != nil {
			return err
		}
		model = &m
	}

	image, err := o.Image.Bytes()
	if err != nil {
		return err
	}

	var mask []byte
	if o.Mask != nil {
		mask, err = o.Mask.Bytes()
		if err != nil {
			return err
		}
	}

	//nolint:govet
	*c = CreateImageEditRequest{
		JobRequest{},
		image,
		mask,
		model,
		o.N,
		o.Prompt,
		(*string)(o.ResponseFormat),
		(*string)(o.Size),
		o.User,
	}

	return nil
}
