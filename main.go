package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/joho/godotenv"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	http.HandleFunc("/live", handleLiveRequest)
	http.HandleFunc("/", serveHTML)
	log.Println("Server is running on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func serveHTML(w http.ResponseWriter, r *http.Request) {
	htmlData, err := os.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Error reading HTML file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	_, err = w.Write(htmlData)
	if err != nil {
		return
	}
}

func handleLiveRequest(w http.ResponseWriter, r *http.Request) {
	camera := r.URL.Query().Get("camera")
	if camera == "" {
		http.Error(w, "No camera specified (front, back, left, right)", http.StatusBadRequest)
		return
	}

	sess, err := session.NewSession(&aws.Config{
		Region:           aws.String(os.Getenv("AWS_REGION")),
		Credentials:      credentials.NewEnvCredentials(),
		Endpoint:         aws.String(os.Getenv("S3_CUSTOM_ENDPOINT")),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	svc := s3.New(sess)

	bucketName := os.Getenv("S3_BUCKET_NAME")
	prefix := os.Getenv("S3_BUCKET_PREFIX")

	loc, _ := time.LoadLocation("Asia/Bangkok")

	currentDate := time.Now().In(loc).Format("2006-01-02")
	fullPrefix := prefix + "/streams/" + currentDate

	startAfter := time.Now().In(loc).Add(-10 * time.Minute).Format("2006-01-02_15-04-05")

	params := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucketName),
		Prefix:  aws.String(fullPrefix),
		MaxKeys: aws.Int64(100),

		StartAfter: aws.String(fullPrefix + "/" + startAfter),
	}

	resp, err := svc.ListObjectsV2(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fileList := resp.Contents

	if len(fileList) > 0 {
		log.Printf("Found %d files", len(fileList))
		re := regexp.MustCompile(`(\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2})`)

		sort.SliceStable(fileList, func(i, j int) bool {
			firstMatch := re.FindStringSubmatch(*fileList[i].Key)[0]
			secondMatch := re.FindStringSubmatch(*fileList[j].Key)[0]

			return strings.Compare(secondMatch, firstMatch) < 0
		})

		fileCount := len(fileList)
		if fileCount > 8 {
			fileCount = 8
		}

		latestFiles := make(map[string]*s3.Object)

		for _, file := range fileList[:fileCount] {
			if strings.Contains(*file.Key, "front") && latestFiles["front"] == nil {
				latestFiles["front"] = file
			} else if strings.Contains(*file.Key, "back") && latestFiles["back"] == nil {
				latestFiles["back"] = file
			} else if strings.Contains(*file.Key, "left") && latestFiles["left"] == nil {
				latestFiles["left"] = file
			} else if strings.Contains(*file.Key, "right") && latestFiles["right"] == nil {
				latestFiles["right"] = file
			}
		}

		var keyToSign *string

		if latestFiles[camera] == nil {
			keyToSign = aws.String("/placeholder.mp4")
		} else {
			keyToSign = latestFiles[camera].Key
		}

		req, _ := svc.GetObjectRequest(&s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    keyToSign,
		})

		urlStr, err := req.Presign(5 * time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, urlStr, http.StatusFound)
		return
	} else {
		http.Error(w, "No files found", http.StatusInternalServerError)
		return
	}
}
