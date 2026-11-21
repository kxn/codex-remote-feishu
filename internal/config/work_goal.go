package config

import "fmt"

const DefaultWorkGoalTokenBudget int64 = 100000

type WorkSettings struct {
	DefaultGoalTokenBudget int64 `json:"defaultGoalTokenBudget,omitempty"`
}

func (s WorkSettings) Validate() error {
	if s.DefaultGoalTokenBudget < 0 {
		return fmt.Errorf("work.defaultGoalTokenBudget must be positive, or omitted for the configured default")
	}
	return nil
}
