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
	"io/ioutil"
	"os"
	"sync"
	"net/http"
	"os/exec"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
	"cloud.google.com/go/firestore"
)

var uploadNum = 0
// default important values
var status = http.StatusOK
var errorMessage = ""
var title = ""
var passphrase = ""
var photos = ""
var numPhotos = 0

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
	types := make([]string, numPhotos)
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
				types[i] = "jpg"
			} else if contentType == "image/png" {
				photoString += ".png"
				types[i] = "png"
			}

			// create temporary file for storing the original photo
			origFile, err := os.Create(photoString)
			if _, err := io.Copy(origFile, photoFile); err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error. Could not save photo to file."
				return
			}
			defer origFile.Close()
			defer os.Remove(photoString)

			if _, err = origFile.Seek(0, io.SeekStart); err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error. Could not seek to beginning of original file."
				return
			}

			// run the photo compression script
			cmd := exec.Command("./webpic", photoString, ".", "3d500w", "3d382w", "3d288w")
			_, err = cmd.Output()
			if err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error. Could not optimize original photo file."
				return
			}

			// get the compressed files, iterate over them
			compressedFiles, err := ioutil.ReadDir(photoName)
			if err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error. Could not iterate over compressed files."
				return
			}
			for _, fileInfo := range compressedFiles {
				compressedFile, err := os.Open(photoName + "/" + fileInfo.Name())
				if err != nil {
					status = http.StatusInternalServerError
					errorMessage = "Error. Could not open compressed file."
					return
				}
				defer compressedFile.Close()
				// upload the compressed file to the storage bucket
				compressedWriter := client.Bucket(bucket).Object("images/gallery/upload" + strconv.Itoa(uploadNum) + "/" + photoName + "/" + fileInfo.Name()).NewWriter(context)
				if _, err := io.Copy(compressedWriter, compressedFile); err != nil {
					status = http.StatusInternalServerError
					errorMessage = "Error: Issue transferring compressed photo to storage bucket."
				}
				if err := compressedWriter.Close(); err != nil {
					status = http.StatusInternalServerError
					errorMessage = "Error: Issue closing connection to storage bucket."
					return
				}
			}

			// remove all the compressed files from local memory
			err = os.RemoveAll(photoName)
			if err != nil {
				status = http.StatusInternalServerError
				errorMessage = "Error: Could not remove directory containing compressed files."
				return
			}

			// upload the original photo to the storage bucket
			origWriter := client.Bucket(bucket).Object("images/raw/gallery/upload" + strconv.Itoa(uploadNum) + "/" + photoString).NewWriter(context)
			if _, err := io.Copy(origWriter, origFile); err != nil {
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

	var captionString string
	var typeString string

	for i, val := range captions {
		if i == 0 {
			captionString += val
		} else {
			captionString += "," + val
		}
	}

	for i, val := range types {
		if i == 0 {
			typeString += val
		} else {
			typeString += "," + val
		}
	}

	// create new photogroup in repository
	cmd := exec.Command("./new-photogroup", strconv.Itoa(uploadNum), title, strconv.Itoa(numPhotos), captionString, typeString)
	stdout, err := cmd.Output()
	if err != nil {
		status = http.StatusInternalServerError
		errorMessage = "Error. Could not create new photogroup in repository."
		return
	}

	// remove website files from memory
	err = os.RemoveAll("growinggreen-site")
	if err != nil {
		status = http.StatusInternalServerError
		errorMessage = "Error: Could not remove website directory."
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
