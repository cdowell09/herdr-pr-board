// Package reviewercontract defines the agent-neutral version-one wire protocol.
package reviewercontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

const Version = 1

type Input struct {
	Version    int                   `json:"version"`
	Identity   reviewmemory.Identity `json:"identity"`
	BaseOID    string                `json:"base_oid"`
	PRURL      string                `json:"pr_url"`
	Title      string                `json:"title"`
	ResultPath string                `json:"result_path"`
}

type Result struct {
	Version  int                   `json:"version"`
	Identity reviewmemory.Identity `json:"identity"`
	BaseOID  string                `json:"base_oid"`
	Outcome  reviewmemory.Outcome  `json:"outcome"`
}

func (in Input) Validate() error {
	if in.Version != Version {
		return errors.New("unsupported reviewer input version")
	}
	if err := reviewmemory.ValidateRevision(in.Identity, in.BaseOID); err != nil {
		return err
	}
	if !filepath.IsAbs(in.ResultPath) {
		return errors.New("reviewer result_path must be absolute")
	}
	return nil
}

func (result Result) Validate(in Input) error {
	if err := in.Validate(); err != nil {
		return err
	}
	if result.Version != Version {
		return errors.New("unsupported reviewer result version")
	}
	if result.Identity != in.Identity || result.BaseOID != in.BaseOID {
		return errors.New("reviewer result revision does not match input")
	}
	return reviewmemory.ValidateOutcome(result.Outcome)
}

// Decode rejects unknown fields and trailing content at the reviewer boundary.
func Decode(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("reviewer JSON contains trailing content")
	}
	return nil
}
