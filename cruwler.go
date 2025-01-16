package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/logrusorgru/aurora"
	"github.com/naufalardhani/cruwler/internal/runner"
	"github.com/projectdiscovery/gologger"
	"golang.org/x/net/html"
)

type Options struct {
	URL           string
	Cookie        string
	Authorization string
	Output        string
	Recursive     bool
}

type Result struct {
	URLs []string `json:"urls"`
}

func parseOptions() *Options {
	options := &Options{}
	flag.StringVar(&options.URL, "url", "", "Target URL to crawl")
	flag.StringVar(&options.Cookie, "cookie", "", "Cookie value if required")
	flag.StringVar(&options.Authorization, "authorization", "", "Authorization header if required")
	flag.StringVar(&options.Output, "output", "", "Output file to save results")
	flag.BoolVar(&options.Recursive, "recursive", false, "Enable recursive crawling")
	flag.Parse()

	if handlePipeInput() {
		os.Exit(0)
	}

	if options.URL == "" {
		fmt.Println()
		gologger.Error().Msg("URL is required. Use -url flag.")
		fmt.Println()
		flag.Usage()
		return nil
	}

	return options
}

func handlePipeInput() bool {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		scanner := bufio.NewScanner(os.Stdin)
		var output string
		for scanner.Scan() {
			url := strings.TrimSpace(scanner.Text())
			if url == "" {
				continue
			}
			output += formatURL(url)
		}
		fmt.Print(output)
		return true
	}
	return false
}

func formatURL(url string) string {
	if hasFileExtension(url) {
		return fmt.Sprintln(aurora.Green(url))
	}
	return fmt.Sprintln(aurora.Blue(url))
}

func crawlURL(baseURL, cookie, authorization string, recursive bool, visitedURLs *sync.Map) ([]string, error) {
	client := &http.Client{}
	req, err := createRequest(baseURL, cookie, authorization)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	urls, err := extractURLs(resp.Body, baseURL)
	if err != nil {
		return nil, err
	}

	var uniqueURLs []string
	for _, u := range urls {
		if _, visited := visitedURLs.LoadOrStore(u, true); !visited {
			uniqueURLs = append(uniqueURLs, u)
		}
	}

	if recursive {
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, u := range uniqueURLs {
			if isSameHost(baseURL, u) {
				wg.Add(1)
				go func(url string) {
					defer wg.Done()
					recursiveURLs, err := crawlURL(url, cookie, authorization, false, visitedURLs)
					if err == nil {
						mu.Lock()
						uniqueURLs = append(uniqueURLs, recursiveURLs...)
						mu.Unlock()
					}
				}(u)
			}
		}
		wg.Wait()
	}

	return uniqueURLs, nil
}

func isSameHost(baseURL, targetURL string) bool {
	base, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	return base.Host == target.Host
}

func createRequest(baseURL, cookie, authorization string) (*http.Request, error) {
	req, err := http.NewRequest("GET", baseURL, nil)
	if err != nil {
		return nil, err
	}

	if cookie != "" {
		req.Header.Add("Cookie", cookie)
	}
	if authorization != "" {
		req.Header.Add("Authorization", authorization)
	}

	return req, nil
}

func extractURLs(body io.Reader, baseURL string) ([]string, error) {
	var urls []string
	z := html.NewTokenizer(body)

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() == io.EOF {
				break
			}
			return nil, z.Err()
		}

		t := z.Token()
		if t.Type == html.StartTagToken {
			urls = append(urls, extractURLFromToken(t, baseURL)...)
		}
	}

	return urls, nil
}

func extractURLFromToken(t html.Token, baseURL string) []string {
	var urls []string
	if isValidTag(t.Data) {
		for _, attr := range t.Attr {
			if attr.Key == "href" || attr.Key == "src" {
				if url := processURL(attr.Val, baseURL); url != "" {
					urls = append(urls, url)
				}
			}
		}
	}
	return urls
}

func isValidTag(tag string) bool {
	return tag == "a" || tag == "link" || tag == "script" || tag == "img"
}

func processURL(foundURL, baseURL string) string {
	if foundURL != "" {
		absoluteURL, err := makeAbsoluteURL(baseURL, foundURL)
		if err == nil {
			return absoluteURL
		}
	}
	return ""
}

func makeAbsoluteURL(baseURL, relativeURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	relative, err := url.Parse(relativeURL)
	if err != nil {
		return "", err
	}

	absolute := base.ResolveReference(relative)
	return absolute.String(), nil
}

func hasFileExtension(urlStr string) bool {
	ext := path.Ext(urlStr)
	return ext != "" && !strings.HasPrefix(ext, ".com") && !strings.HasPrefix(ext, ".org") && !strings.HasPrefix(ext, ".net")
}

func processFileInput(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error reading file: %v", err)
	}

	urls := strings.Split(string(content), "\n")
	var output string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u != "" {
			output += u + "\n"
		}
	}
	fmt.Print(output)
	return nil
}

func writeOutput(urls []string, options *Options) error {
	if options.Output != "" && strings.HasSuffix(options.Output, ".json") {
		return writeJSONOutput(urls, options.Output)
	}
	return writeTextOutput(urls, options.Output)
}

func writeJSONOutput(urls []string, outputPath string) error {
	result := Result{URLs: urls}
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("error creating JSON: %v", err)
	}

	return os.WriteFile(outputPath, jsonData, 0644)
}

func writeTextOutput(urls []string, outputPath string) error {
	var output string
	for _, u := range urls {
		if outputPath != "" {
			output += u + "\n"
		} else {
			if hasFileExtension(u) {
				fmt.Println(aurora.Green(u))
			} else {
				fmt.Println(aurora.Blue(u))
			}
		}
	}

	if outputPath != "" {
		return os.WriteFile(outputPath, []byte(output), 0644)
	}
	return nil
}

func main() {
	startTime := time.Now()
	runner.ShowBanner()

	options := parseOptions()
	if options == nil {
		return
	}

	// Check if URL is a file path
	if _, err := os.Stat(options.URL); err == nil {
		if err := processFileInput(options.URL); err != nil {
			gologger.Error().Msgf("Error processing file: %v", err)
		}
		return
	}

	visitedURLs := &sync.Map{}
	visitedURLs.Store(options.URL, true)

	urls, err := crawlURL(options.URL, options.Cookie, options.Authorization, options.Recursive, visitedURLs)
	if err != nil {
		gologger.Error().Msgf("Error crawling: %v", err)
		return
	}

	// Remove duplicates
	uniqueURLs := make([]string, 0)
	seen := make(map[string]bool)
	for _, u := range urls {
		if !seen[u] {
			seen[u] = true
			uniqueURLs = append(uniqueURLs, u)
		}
	}

	if err := writeOutput(uniqueURLs, options); err != nil {
		gologger.Error().Msgf("Error writing output: %v", err)
	}

	elapsedTime := time.Since(startTime).Seconds()

	fmt.Println()
	gologger.Info().Msgf("Total URLs found: %d", len(uniqueURLs))
	gologger.Info().Msgf("Execution Time: %.2f sec", elapsedTime)
}
