package gpustatus

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

const defaultBinary = "nvidia-smi"

var queryArgs = []string{
	"--query-gpu=index,name,temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw,power.limit",
	"--format=csv,noheader,nounits",
}

type GPU struct {
	Index              int
	Name               string
	TemperatureC       float64
	UtilizationPercent float64
	MemoryUsedMiB      float64
	MemoryTotalMiB     float64
	PowerDrawW         float64
	PowerLimitW        float64
}

type Snapshot struct {
	CollectedAt time.Time
	GPUs        []GPU
}

func Collect(ctx context.Context) (Snapshot, error) {
	output, err := execlaunch.CommandContext(ctx, defaultBinary, queryArgs...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return Snapshot{}, fmt.Errorf("nvidia-smi: %s", message)
	}
	gpus, err := Parse(output)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{CollectedAt: time.Now(), GPUs: gpus}, nil
}

func Parse(output []byte) ([]GPU, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimSpace(string(output))))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse nvidia-smi csv: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("nvidia-smi returned no GPU rows")
	}
	gpus := make([]GPU, 0, len(records))
	for rowIndex, record := range records {
		if len(record) != 8 {
			return nil, fmt.Errorf("parse nvidia-smi row %d: expected 8 fields, got %d", rowIndex+1, len(record))
		}
		index, err := parseInt(record[0])
		if err != nil {
			return nil, fmt.Errorf("parse nvidia-smi row %d index: %w", rowIndex+1, err)
		}
		gpu := GPU{
			Index: index,
			Name:  strings.TrimSpace(record[1]),
		}
		values := []*float64{
			&gpu.TemperatureC,
			&gpu.UtilizationPercent,
			&gpu.MemoryUsedMiB,
			&gpu.MemoryTotalMiB,
			&gpu.PowerDrawW,
			&gpu.PowerLimitW,
		}
		for valueIndex, target := range values {
			value, err := parseFloat(record[valueIndex+2])
			if err != nil {
				return nil, fmt.Errorf("parse nvidia-smi row %d field %d: %w", rowIndex+1, valueIndex+3, err)
			}
			*target = value
		}
		gpus = append(gpus, gpu)
	}
	return gpus, nil
}

func parseInt(value string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(value))
}

func parseFloat(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.EqualFold(trimmed, "N/A") || trimmed == "[Not Supported]" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", trimmed)
	}
	return parsed, nil
}
