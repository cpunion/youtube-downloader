package progress

import (
	"fmt"
	"io"
	"time"

	"github.com/cheggaaa/pb/v3"
)

type Task struct {
	Name        string
	Total       int64
	Current     int64
	Bar         *pb.ProgressBar
	IsMergeTask bool
}

func NewTask(name string, total int64, isMergeTask bool) *Task {
	bar := pb.New64(total).Set("prefix", name).SetRefreshRate(time.Millisecond * 100)
	if isMergeTask {
		// Merge task progress bar format: task name: percentage progress, progress bar, (ffmpeg processed time/total time), task remaining time
		bar.SetTemplateString(`{{with string . "prefix"}}{{.}}{{end}}: {{percent . }} {{bar . }} {{string . "counters" }} [{{string . "speed"}}] {{rtime . "ETA %s"}}{{with string . "suffix"}} {{.}}{{end}}`)
	} else {
		// Download task progress bar format: task name: percentage progress, progress bar, (downloaded bytes/total bytes), task remaining time
		bar.Set(pb.Bytes, true)
		bar.SetTemplateString(`{{with string . "prefix"}}{{.}}{{end}}: {{percent . }} {{bar . }} {{counters . }} [{{speed . }}] {{rtime . "ETA %s"}}{{with string . "suffix"}} {{.}}{{end}}`)
	}

	bar.Start()

	if bar.Err() != nil {
		panic(bar.Err())
	}

	return &Task{
		Name:        name,
		Total:       total,
		IsMergeTask: isMergeTask,
		Bar:         bar,
	}
}

func (t *Task) Reader(r io.Reader) io.Reader {
	return t.Bar.NewProxyReader(r)
}

func (t *Task) SetCurrent(current int64) {
	if current > t.Total {
		current = t.Total
	}
	t.Current = current
	t.Bar.SetCurrent(current)
	if t.IsMergeTask {
		counters := fmt.Sprintf("%s/%s", formatDuration(time.Duration(t.Current)), formatDuration(time.Duration(t.Total)))
		t.Bar.Set("counters", counters)
	}
}

func (t *Task) SetTotal(total int64) {
	t.Total = total
	t.Bar.SetTotal(total)
}

func (t *Task) SetSpeed(speed float64) {
	if t.IsMergeTask {
		t.Bar.Set("speed", fmt.Sprintf("%.2fx", speed))
	}
}

func (t *Task) Finish() {
	t.Bar.Finish()
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh:%dm:%ds", h, m, s)
	} else if m > 0 {
		return fmt.Sprintf("%dm:%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
