/*
* photouploader takes files sent via form and uploads them to a google cloud storage bucket.
* The form is passphrase protected, and allows for setting of photo captions and title of group.
* Upon uploading, the firestore database is also updated with the relevant information.
*/
package photouploader

import (
	"log"
	"strconv"
	"context"
	"io"
	"errors"
	"os"
	"sync"
	"net/http"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
	"github.com/presence-web-services/gmailer/v2"
	"github.com/joho/godotenv"
)

var config gmailer.Config
var uploadNum = 0
// default important values
var status = http.StatusOK
var errorMessage = ""
var title = ""
var passphrase = ""
var photos = ""
var numPhotos = 0

// init loads environment variables and authenticates the gmailer config
func init() {
	loadEnvVars()
	authenticate()
}

// loadEnvVars loads environment variables from a .env file
func loadEnvVars() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error: Could not load environment variables from .env file.")
	}
	config.ClientID = os.Getenv("CLIENT_ID")
	config.ClientSecret = os.Getenv("CLIENT_SECRET")
	config.AccessToken = os.Getenv("ACCESS_TOKEN")
	config.RefreshToken = os.Getenv("REFRESH_TOKEN")
	config.EmailTo = os.Getenv("EMAIL_TO")
	config.EmailFrom = os.Getenv("EMAIL_FROM")
	config.ReplyTo = os.Getenv("EMAIL_FROM")
	config.Subject = os.Getenv("SUBJECT")
	if os.Getenv("HP") == "true" {
		config.HP = true
	} else {
		config.HP = false
	}
}

// authenticate authenticates a gmailer config
func authenticate() {
	err := config.Authenticate()
	if err != nil {
		log.Fatal("Error: Could not authenticate with GMail OAuth using credentials.")
	}
}

// CreateAndRun creates a http server that listens for photo data. You may set the upload beginning offset here.
func CreateAndRun(port string, uploadOffset int) {
	uploadNum = uploadOffset;
	http.HandleFunc("/", handler)
	http.ListenAndServe(":"+port, nil)
}

// handler checks the title and passphrase for sanity, and then copies the uploaded photos to a google storage bucket
func handler(response http.ResponseWriter, request *http.Request) {
	defaultValues()
	verifyPost(response, request.Method)
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	getFormData(request)
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	checkTitle()
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	checkPassphrase()
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	copyPhotosToBucket(request)
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	response.Write([]byte("Photos uploaded successfully!"))
	uploadNum++
}

// defaultValues sets the status, errorMessage, title, passphrase, numphotos to default values
func defaultValues() {
	status = http.StatusOK
	errorMessage = ""
	title = ""
	passphrase = ""
	numPhotos = 0
	config.Body = ""
}

// verifyPost ensures that a POST is sent
func verifyPost(response http.ResponseWriter, method string) {
	if method != "POST" {
		response.Header().Set("Allow", "POST")
		status = http.StatusMethodNotAllowed
		errorMessage = "Error: Method " + method + " not allowed. Only POST allowed."
	}
}

// getFormData populates config struct and hp variable with POSTed data from form submission
func getFormData(request *http.Request) {
	title = request.PostFormValue("title")
	passphrase = request.PostFormValue("passphrase")
	var err error
	numPhotos, err = strconv.Atoi(request.PostFormValue("numPhotos"))
	if err != nil {
		status = http.StatusBadRequest;
		errorMessage = "Error: Could not determine number of photos uploaded."
	}
}

// checkTitle ensures the title is filled out
func checkTitle() {
	if title == "" {
		status = http.StatusBadRequest
		errorMessage = "Error: Title not defined."
	}
}

// checkPassphrase checks that the passphrase is correct
func checkPassphrase() {
	if passphrase != "care for your surroundings" {
		status = http.StatusUnauthorized
		errorMessage = "Error: Passphrase incorrect."
	}
}

// copyPhotosToBucket copies all photos uplaoded to the google storage bucket, and updates the firestore DB with the relevant info
func copyPhotosToBucket(request *http.Request) {
	bucket := "growinggreen-assets"
	context := context.Background()
	// create new google storage bucket client
	client, err := storage.NewClient(context, option.WithCredentialsFile("asset-bucket-sa-key.json"))
	if err != nil {
		status = http.StatusInternalServerError
		errorMessage = "Error: Issue connecting to storage bucket."
		return
	}
	defer client.Close()

	var wg sync.WaitGroup
	captions := make([]string, numPhotos)
	photoStrings := make([]string, numPhotos)
	// iterate over all photos uploaded
	for i := 0; i < numPhotos; i++ {
		wg.Add(1)
		go func(i int) {
			captionName := "caption" + strconv.Itoa(i)
			photoName := "photo" + strconv.Itoa(i)
			caption := request.PostFormValue(captionName)
			captions[i] = caption
			_, header, err := request.FormFile(photoName)
			if err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error: Could not read photo file uploaded."
				return
			}
			contentType := header.Header["Content-Type"][0]
			err = checkPhoto(header.Size, contentType)
			if err != nil {
				status = http.StatusInternalServerError
				errorMessage = err.Error()
				return
			}
			_, ok := header.Header["Content-Disposition"]
			if ok {
				delete(header.Header, "Content-Disposition")
			}

			photoFile, err := header.Open()
			if err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error. Could not open photo file for reading."
				return
			}
			defer photoFile.Close()

			photoString := photoName
			if contentType == "image/jpeg" {
				photoString += ".jpg"
			} else if contentType == "image/png" {
				photoString += ".png"
			}
			photoStrings[i] = photoString

			// upload the original photo to the storage bucket
			origWriter := client.Bucket(bucket).Object("images/raw/gallery/upload" + strconv.Itoa(uploadNum) + "/" + photoString).NewWriter(context)
			if _, err := io.Copy(origWriter, photoFile); err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error: Issue transferring photo header to storage bucket."
			}
			if err := origWriter.Close(); err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error: Issue closing connection to storage bucket."
				return
			}
			wg.Done()
		}(i)
	}
	wg.Wait()
	config.Body += "Upload: " + strconv.Itoa(uploadNum) + "\n"
	config.Body += "Title: " + title + "\n"
	for i := 0; i < numPhotos; i++ {
		config.Body += photoStrings[i] + ": " + captions[i] + "\n"
	}
	sendEmail()
}

// checkPhoto checks that the size of the photo is not 0, and makes sure the content type is JPEG or PNG
func checkPhoto(size int64, contentType string) (err error) {
	if size == 0 {
		err = errors.New("Error: Size of photo is 0.")
		return
	}
	if contentType != "image/jpeg" && contentType != "image/png" {
		err = errors.New("Error: Photo is not JPEG or PNG.")
		return
	}
	return
}

// sendEmail sends an email given a gmailer config
func sendEmail() {
	err := config.Send()
	if err != nil {
		status = http.StatusInternalServerError
		errorMessage = "Error: Internal server error."
		return
	}
}
