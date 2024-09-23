package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cpunion/video-downloader/extractor"
)

func main() {
	// Define command line parameters
	overwrite := flag.Bool("y", false, "Overwrite existing files without prompting")
	flag.Parse()

	// Check if URL parameter is provided
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Please provide a YouTube URL")
		os.Exit(1)
	}

	url := args[0]
	downloader := extractor.NewYouTubeDownloader(url)

	// Set the option to overwrite files
	downloader.SetOverwrite(*overwrite)

	err := downloader.GetVideoInfo()
	if err != nil {
		fmt.Printf("Error getting video info: %v\n", err)
		os.Exit(1)
	}

	err = downloader.Download()
	if err != nil {
		fmt.Printf("Error downloading video: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Download completed successfully")
}
