package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func SendRequest(client *http.Client, req *http.Request, respObj any) (int, error) {
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}

	defer res.Body.Close()

	statusCode := res.StatusCode
	if statusCode < http.StatusOK || statusCode >= http.StatusBadRequest {
		return statusCode, decodeError(res)
	}

	if err = json.NewDecoder(res.Body).Decode(respObj); err != nil {
		return http.StatusInternalServerError, err
	}

	return statusCode, nil
}

func decodeError(resp *http.Response) error {
	s, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read body for error response: %w", err)
	}

	return fmt.Errorf("%s", s)
}
