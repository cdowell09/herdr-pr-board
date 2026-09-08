package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/cdowell09/herdr-pr-board/internal/reviewinstructions"
)

// ReviewerEdit creates a profile or updates instruction selections on an existing profile.
// Expected must match the profile shown during setup; nil requires a new profile.
type ReviewerEdit struct {
	Value    Reviewer
	Expected *Reviewer
}

func (r Reviewer) Equal(other Reviewer) bool { return reflect.DeepEqual(r, other) }

// LoadInstructions resolves profile paths against the configuration directory.
// InstructionFiles preserves legacy command paths relative to the launcher.
func (r Reviewer) LoadInstructions(configPath string) (reviewinstructions.Instructions, error) {
	prompt, skill, err := r.InstructionFiles()
	if err != nil {
		return reviewinstructions.Instructions{}, fmt.Errorf("reviewer %s: %w", r.ID, err)
	}
	loaded, err := (reviewinstructions.Files{Prompt: prompt, Skill: skill}).Load(filepath.Dir(configPath))
	if err != nil {
		return reviewinstructions.Instructions{}, fmt.Errorf("reviewer %s: %w", r.ID, err)
	}
	return loaded, nil
}

func editReviewer(data []byte, reviewers []Reviewer, edit ReviewerEdit) ([]byte, error) {
	r := edit.Value
	index := slices.IndexFunc(reviewers, func(existing Reviewer) bool { return existing.ID == r.ID })
	if edit.Expected == nil {
		if index >= 0 {
			return nil, fmt.Errorf("reviewer %s already exists; reload repository settings", r.ID)
		}
		id, _ := json.Marshal(r.ID)
		command, _ := json.Marshal(r.Command)
		text := "\n\n[[reviewers]]\nid = " + string(id) + "\ncommand = " + string(command) + "\n"
		for _, field := range reviewerFields(r) {
			text += field.key + " = " + field.value + "\n"
		}
		return append(append([]byte(nil), data...), []byte(text)...), nil
	}
	if index < 0 || !reviewers[index].Equal(*edit.Expected) {
		return nil, fmt.Errorf("reviewer %s changed; reload repository settings", r.ID)
	}
	if r.ID != edit.Expected.ID || !slices.Equal(r.Command, edit.Expected.Command) {
		return nil, errors.New("repository settings must preserve the reviewer ID and command")
	}
	return editTableFields(data, "reviewers", index, reviewerFields(r))
}

func reviewerFields(r Reviewer) []repositoryField {
	var fields []repositoryField
	for _, value := range []struct {
		key  string
		path *string
	}{{"prompt_file", r.PromptFile}, {"skill_file", r.SkillFile}} {
		if value.path != nil {
			encoded, _ := json.Marshal(*value.path)
			fields = append(fields, repositoryField{value.key, string(encoded)})
		}
	}
	return fields
}
