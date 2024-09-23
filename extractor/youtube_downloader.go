package extractor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/cpunion/video-downloader/progress"
	"github.com/kkdai/youtube/v2"
)

type Video struct {
	ID       string
	Title    string
	URL      string
	Filename string
}

type YouTubeDownloader struct {
	Type      string
	URL       string
	Videos    []Video
	MaxRes    int
	OnlyMP4   bool
	overwrite bool
}

func NewYouTubeDownloader(url string) *YouTubeDownloader {
	return &YouTubeDownloader{
		Type:    "youtube",
		URL:     url,
		MaxRes:  1080,
		OnlyMP4: true,
	}
}

func (yd *YouTubeDownloader) GetVideoInfo() error {
	videoID := yd.extractVideoID()
	if videoID == "" {
		return fmt.Errorf("invalid YouTube URL")
	}

	title, err := yd.fetchVideoTitle(videoID)
	if err != nil {
		return err
	}

	yd.Videos = append(yd.Videos, Video{
		ID:    videoID,
		Title: title,
		URL:   yd.URL,
	})

	return nil
}

func (yd *YouTubeDownloader) fetchVideoTitle(videoID string) (string, error) {
	apiURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	body, err := yd.fetchURL(apiURL)
	if err != nil {
		return "", err
	}

	videoDetails, err := yd.extractVideoDetails(body)
	if err != nil {
		return "", err
	}

	title, ok := videoDetails["title"].(string)
	if !ok {
		return "", fmt.Errorf("failed to extract title from videoDetails")
	}

	return title, nil
}

func (yd *YouTubeDownloader) extractVideoDetails(body []byte) (map[string]interface{}, error) {
	re := regexp.MustCompile(`var ytInitialPlayerResponse = ({.*?});`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return nil, fmt.Errorf("failed to find ytInitialPlayerResponse in the page")
	}

	var videoDetails map[string]interface{}
	err := json.Unmarshal(matches[1], &videoDetails)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal video details: %w", err)
	}

	details, ok := videoDetails["videoDetails"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("videoDetails does not contain 'videoDetails' key or it's not a map")
	}

	return details, nil
}

func (yd *YouTubeDownloader) Download() error {
	client := youtube.Client{}

	for _, video := range yd.Videos {
		fmt.Printf("Downloading video: %s\n", video.Title)

		videoObj, err := client.GetVideo(video.ID)
		if err != nil {
			return fmt.Errorf("failed to get video info: %w", err)
		}

		yd.printAvailableFormats(videoObj.Formats)

		videoFormat, audioFormat, err := yd.selectFormats(videoObj.Formats)
		if err != nil {
			return err
		}

		yd.printSelectedFormats(videoFormat, audioFormat)

		err = yd.downloadAndMerge(client, videoObj, videoFormat, audioFormat, video.Title)
		if err != nil {
			return err
		}
	}
	return nil
}

func (yd *YouTubeDownloader) downloadAndMerge(client youtube.Client, videoObj *youtube.Video, videoFormat youtube.Format, audioFormat *youtube.Format, title string) error {
	safeTitle := makeSafeFilename(title)
	videoFilename := fmt.Sprintf("%s_video.mp4", safeTitle)
	audioFilename := fmt.Sprintf("%s_audio.mp4", safeTitle)
	outputFilename := fmt.Sprintf("%s.mp4", safeTitle)

	// Check if the video file already exists and has the correct size
	videoExists, err := yd.checkFileExists(videoFilename, videoFormat.ContentLength)
	if err != nil {
		return err
	}

	if !videoExists {
		videoTask := progress.NewTask("Video", videoFormat.ContentLength, false)
		err := yd.downloadStream(client, videoObj, &videoFormat, videoFilename, videoTask)
		if err != nil {
			fmt.Printf("Error downloading video: %v\n", err)
		}
		videoTask.Finish()
	} else {
		fmt.Printf("Video file already exists and has correct size. Skipping download.\n")
	}

	var audioExists bool
	if audioFormat != nil {
		// Check if the audio file already exists and has the correct size
		audioExists, err = yd.checkFileExists(audioFilename, audioFormat.ContentLength)
		if err != nil {
			return err
		}

		if !audioExists {
			audioTask := progress.NewTask("Audio", audioFormat.ContentLength, false)
			err := yd.downloadStream(client, videoObj, audioFormat, audioFilename, audioTask)
			if err != nil {
				fmt.Printf("Error downloading audio: %v\n", err)
			}
			audioTask.Finish()
		} else {
			fmt.Printf("Audio file already exists and has correct size. Skipping download.\n")
		}
	}

	targetExists, err := yd.checkFileExists(outputFilename, videoFormat.ContentLength+audioFormat.ContentLength)
	if err != nil {
		return err
	}

	if !targetExists {
		fmt.Println("\nMerging video and audio...")
		mergeTask := progress.NewTask("Merging", int64(videoObj.Duration), true)
		err = yd.mergeVideoAudio(videoFilename, audioFilename, outputFilename, mergeTask)
		if err != nil {
			return err
		}
		mergeTask.Finish()

		if !yd.overwrite {
			os.Remove(videoFilename)
			os.Remove(audioFilename)
		}
	} else if !videoExists {
		err = os.Rename(videoFilename, outputFilename)
		if err != nil {
			return fmt.Errorf("failed to rename video file: %w", err)
		}
		fmt.Println("\nVideo already includes audio.")
	}

	fmt.Printf("\nDownload completed for: %s\n", outputFilename)
	return nil
}

// Add this new helper function
func (yd *YouTubeDownloader) checkFileExists(filename string, expectedSize int64) (bool, error) {
	info, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.Size() == expectedSize, nil
}

func (yd *YouTubeDownloader) mergeVideoAudio(videoFilename, audioFilename, outputFilename string, task *progress.Task) error {
	// Construct FFmpeg command
	ffmpegArgs := []string{
		"-y",
		"-i", videoFilename,
		"-i", audioFilename,
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		outputFilename,
	}

	// Print FFmpeg command line
	fmt.Println("Executing FFmpeg command:")
	fmt.Printf("ffmpeg %s\n", strings.Join(ffmpegArgs, " "))

	cmd := exec.Command("ffmpeg", ffmpegArgs...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Use MultiReader to combine stdout and stderr
	combinedOutput := io.MultiReader(stdout, stderr)

	go progress.ParseFFmpegProgress(combinedOutput, task)

	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("failed to merge and encode video and audio: %w", err)
	}

	fmt.Println("\nVideo and audio merged and encoded successfully.")
	return nil
}

func makeSafeFilename(filename string) string {
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	safe := re.ReplaceAllString(filename, "_")

	safe = strings.TrimSpace(safe)

	if safe == "" {
		safe = "video"
	}

	if len(safe) > 200 {
		safe = safe[:200]
	}

	return safe
}

func (yd *YouTubeDownloader) getVideoDownloadURL(videoID string) (string, error) {
	apiURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch video page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	re := regexp.MustCompile(`var ytInitialPlayerResponse = ({.*?});`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("failed to find ytInitialPlayerResponse in the page")
	}

	var playerResponse map[string]interface{}
	err = json.Unmarshal(matches[1], &playerResponse)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal player response: %w", err)
	}

	streamingData, ok := playerResponse["streamingData"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("failed to find streamingData in player response")
	}

	formats, ok := streamingData["formats"].([]interface{})
	if !ok || len(formats) == 0 {
		return "", fmt.Errorf("no formats found in streamingData")
	}

	var bestFormat map[string]interface{}
	var maxBitrate int64 = 0

	for _, f := range formats {
		format, ok := f.(map[string]interface{})
		if !ok {
			continue
		}

		bitrate, ok := format["bitrate"].(float64)
		if !ok {
			continue
		}

		if int64(bitrate) > maxBitrate {
			maxBitrate = int64(bitrate)
			bestFormat = format
		}
	}

	if bestFormat == nil {
		return "", fmt.Errorf("no suitable format found")
	}

	url, ok := bestFormat["url"].(string)
	if !ok {
		return "", fmt.Errorf("failed to find URL in format")
	}

	return url, nil
}

func (yd *YouTubeDownloader) getThumbnailURL(videoID string) (string, error) {
	apiURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch video page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	re := regexp.MustCompile(`"thumbnails":\[{"url":"(.*?)"`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("failed to find thumbnail URL in the page")
	}

	thumbnailURL := string(matches[1])
	return thumbnailURL, nil
}

func (yd *YouTubeDownloader) extractVideoID() string {
	patterns := []string{
		`(?:v=|\/)([0-9A-Za-z_-]{11}).*`,
		`^([0-9A-Za-z_-]{11})$`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(yd.URL)
		if len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

func (yd *YouTubeDownloader) fetchURL(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

func (yd *YouTubeDownloader) SetOverwrite(overwrite bool) {
	yd.overwrite = overwrite
}

func (yd *YouTubeDownloader) printAvailableFormats(formats []youtube.Format) {
	fmt.Println("Available formats:")
	for _, format := range formats {
		fmt.Printf("ItagNo: %d, Quality: %s, MimeType: %s, Bitrate: %d, AudioChannels: %d\n",
			format.ItagNo, format.QualityLabel, format.MimeType, format.Bitrate, format.AudioChannels)
	}
	fmt.Println()
}

func (yd *YouTubeDownloader) selectFormats(formats []youtube.Format) (youtube.Format, *youtube.Format, error) {
	videoFormat, err := yd.selectVideoFormat(formats)
	if err != nil {
		return youtube.Format{}, nil, err
	}

	var audioFormat *youtube.Format
	if videoFormat.AudioChannels == 0 {
		audioFormat, err = yd.selectAudioFormat(formats)
		if err != nil {
			return youtube.Format{}, nil, err
		}
	}

	return videoFormat, audioFormat, nil
}

func (yd *YouTubeDownloader) selectVideoFormat(formats []youtube.Format) (youtube.Format, error) {
	var videoFormat youtube.Format
	var maxVideoQuality int
	for _, format := range formats {
		if strings.HasPrefix(format.MimeType, "video/") {
			quality := 0
			if format.QualityLabel != "" {
				quality, _ = strconv.Atoi(strings.TrimSuffix(format.QualityLabel, "p"))
			}
			if quality > maxVideoQuality && quality <= yd.MaxRes {
				maxVideoQuality = quality
				videoFormat = format
			}
		}
	}

	if maxVideoQuality == 0 {
		return youtube.Format{}, fmt.Errorf("no suitable video format found")
	}

	return videoFormat, nil
}

func (yd *YouTubeDownloader) selectAudioFormat(formats []youtube.Format) (*youtube.Format, error) {
	var audioFormat *youtube.Format
	var maxAudioBitrate int
	for _, format := range formats {
		if strings.HasPrefix(format.MimeType, "audio/") {
			if format.Bitrate > maxAudioBitrate {
				maxAudioBitrate = format.Bitrate
				audioFormat = &format
			}
		}
	}

	if audioFormat == nil {
		return nil, fmt.Errorf("no suitable audio format found")
	}

	return audioFormat, nil
}

func (yd *YouTubeDownloader) printSelectedFormats(videoFormat youtube.Format, audioFormat *youtube.Format) {
	fmt.Printf("Selected video format: ItagNo: %d, Quality: %s, MimeType: %s, Bitrate: %d\n",
		videoFormat.ItagNo, videoFormat.QualityLabel, videoFormat.MimeType, videoFormat.Bitrate)
	if audioFormat != nil {
		fmt.Printf("Selected audio format: ItagNo: %d, MimeType: %s, Bitrate: %d\n",
			audioFormat.ItagNo, audioFormat.MimeType, audioFormat.Bitrate)
	}
	fmt.Println()
}

func (yd *YouTubeDownloader) downloadStream(client youtube.Client, videoObj *youtube.Video, format *youtube.Format, filename string, task *progress.Task) error {
	stream, _, err := client.GetStream(videoObj, format)
	if err != nil {
		return fmt.Errorf("failed to get stream: %w", err)
	}
	defer stream.Close()

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Use ProxyReader to wrap stream and update the progress bar
	proxyReader := task.Reader(stream)

	_, err = io.Copy(file, proxyReader)
	if err != nil {
		return fmt.Errorf("failed to save stream: %w", err)
	}

	return nil
}
