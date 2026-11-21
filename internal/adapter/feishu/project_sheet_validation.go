package feishu

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// ValidateProjectSheetWrite performs the same local rectangle checks as the
// actual v2 write, without any network operation or invented revision lock.
func ValidateProjectSheetWrite(req ProjectSheetWriteRequest) error {
	_, rows, cols, err := projectSheetRange(req.SpreadsheetToken, req.SheetID, req.Range)
	if err != nil {
		return err
	}
	if rows > 5000 || cols > 100 {
		return fmt.Errorf("sheet write supports at most5000 rows and100 columns per explicit range")
	}
	if len(req.Values) != rows {
		return fmt.Errorf("values row count must match explicit range")
	}
	for _, row := range req.Values {
		for _, cell := range row {
			if text, ok := cell.(string); ok && utf8.RuneCountInString(text) > 50000 {
				return fmt.Errorf("sheet cell text exceeds50000 characters")
			}
			if _, err := json.Marshal(cell); err != nil {
				return fmt.Errorf("sheet cell is not a JSON platform value: %w", err)
			}
		}
		if len(row) != cols {
			return fmt.Errorf("values column count must match explicit range")
		}
	}
	return nil
}
