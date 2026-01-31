package api

import (
	"asset-server/generated/api"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
)

type expoUpdateMultipartResponse struct {
	PartName string `json:"partName"`
	Payload  any    `json:"payload"`
}

func (resp *expoUpdateMultipartResponse) VisitGetExpoUpdateResponse(w http.ResponseWriter) error {
	headers := api.GetExpoUpdate200ResponseHeaders{
		ExpoProtocolVersion: "1",
		ExpoSfvVersion:      "0",
		CacheControl:        "private, max-age=0",
	}

	body := func(w *multipart.Writer) error {
		partWriter, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": []string{"form-data; name=" + resp.PartName},
			"Content-Type":        []string{"application/json"},
		})
		if err != nil {
			return fmt.Errorf("failed to create part: %w", err)
		}

		jsonEncoder := json.NewEncoder(partWriter)
		jsonEncoder.SetEscapeHTML(false)

		err = jsonEncoder.Encode(resp.Payload)
		if err != nil {
			return fmt.Errorf("failed to JSON encode payload: %w", err)
		}

		return nil
	}

	apiResp := api.GetExpoUpdate200MultipartResponse{
		Body:    body,
		Headers: headers,
	}

	return apiResp.VisitGetExpoUpdateResponse(w)
}
