package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// SendRequest sends a request, decodes the response into respObj, and returns the status code and any error that occurred.
func SendRequest(client *http.Client, req *http.Request, respObj any) (code int, err error) {
	var res *http.Response
	res, err = client.Do(req)
	if err != nil {
		return 0, err
	}

	defer func() {
		err = errors.Join(err, res.Body.Close())
	}()

	code = res.StatusCode
	if code < http.StatusOK || code >= http.StatusBadRequest {
		return code, decodeError(res)
	}

	if err := json.NewDecoder(res.Body).Decode(respObj); err != nil {
		return http.StatusInternalServerError, err
	}

	return code, nil
}

func decodeError(resp *http.Response) error {
	s, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read body for error response: %w", err)
	}

	return fmt.Errorf("%s", s)
}
