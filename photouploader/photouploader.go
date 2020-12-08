/*
* photouploader takes photo files sent via passphrase protected form and
* uploads them to a google cloud storage bucket.
 */
package photouploader

import (
	"context"
	"errors"
	"io"
	"net/http"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// default important values
var status = http.StatusOK
var errorMessage = ""

// CreateAndRun creates a http server that listens for photo data.
func CreateAndRun(port string) {
	http.HandleFunc("/", handler)
	http.ListenAndServe(":"+port, nil)
}

// handler checks for the proper passphrase, and then copies the uploaded photos to a google storage bucket
func handler(response http.ResponseWriter, request *http.Request) {
	defaultValues()
	verifyPost(response, request.Method)
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	checkPassphrase(request.PostFormValue("passphrase"))
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	copyPhotoToBucket(request)
	if status != http.StatusOK {
		http.Error(response, errorMessage, status)
		return
	}
	response.Write([]byte("Photos uploaded successfully!"))
}

// defaultValues sets the status, errorMessage, to default values
func defaultValues() {
	status = http.StatusOK
	errorMessage = ""
}

// verifyPost ensures that a POST is sent
func verifyPost(response http.ResponseWriter, method string) {
	if method != "POST" {
		response.Header().Set("Allow", "POST")
		status = http.StatusMethodNotAllowed
		errorMessage = "Error: Method " + method + " not allowed. Only POST allowed."
	}
}

// checkPassphrase checks that the passphrase is correct
func checkPassphrase(passphrase string) {
	if passphrase != "care for your surroundings" {
		status = http.StatusUnauthorized
		errorMessage = "Error: Passphrase incorrect."
	}
}

// copyPhotoToBucket copies the photo uplaoded to the google storage bucket
func copyPhotoToBucket(request *http.Request) {
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

	date := request.PostFormValue("date")
	index := request.PostFormValue("index")
	_, header, err := request.FormFile("photo")
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

	photoString := "photo" + index
	if contentType == "image/jpeg" {
		photoString += ".jpg"
	} else if contentType == "image/png" {
		photoString += ".png"
	}

	// upload the original photo to the storage bucket
	origWriter := client.Bucket(bucket).Object("images/raw/gallery/" + date + "/" + photoString).NewWriter(context)
	if _, err := io.Copy(origWriter, photoFile); err != nil {
		status = http.StatusInternalServerError
		errorMessage = "Error: Issue transferring photo header to storage bucket."
	}
	if err := origWriter.Close(); err != nil {
		status = http.StatusInternalServerError
		errorMessage = "Error: Issue closing connection to storage bucket."
		return
	}
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
