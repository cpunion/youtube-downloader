package progress

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func ParseFFmpegProgress(reader io.Reader, task *Task) {
	scanner := bufio.NewScanner(reader)
	var currentTime time.Duration

	// Regular expression to match Duration line
	durationRegex := regexp.MustCompile(`Duration: (\d+):(\d+):(\d+)\.(\d+)`)

	for scanner.Scan() {
		line := scanner.Text()

		// Check if it is a Duration line
		if matches := durationRegex.FindStringSubmatch(line); matches != nil {
			hours, _ := strconv.Atoi(matches[1])
			minutes, _ := strconv.Atoi(matches[2])
			seconds, _ := strconv.Atoi(matches[3])
			milliseconds, _ := strconv.Atoi(matches[4])

			totalDuration := time.Duration(hours)*time.Hour +
				time.Duration(minutes)*time.Minute +
				time.Duration(seconds)*time.Second +
				time.Duration(milliseconds)*time.Millisecond

			task.SetTotal(int64(totalDuration))
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "out_time_ms":
			ms, err := strconv.ParseInt(value, 10, 64)
			if err == nil {
				currentTime = time.Duration(ms/1000) * time.Millisecond
				task.SetCurrent(int64(currentTime))
			}
		case "speed":
			speed := parseSpeed(value)
			task.SetSpeed(speed)
		case "progress":
			if value == "end" {
				task.SetCurrent(task.Total)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading ffmpeg output: %v\n", err)
	}
}

func parseSpeed(s string) float64 {
	s = strings.TrimSuffix(s, "x")
	speed, _ := strconv.ParseFloat(s, 64)
	return speed
}
