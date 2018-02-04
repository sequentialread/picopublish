package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
  "strings"
  "io"
	"os"
  "bytes"
	"path/filepath"
)

var secretPassword = ""
var dataPath = ""

func indexHtml(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		response.WriteHeader(404)
		fmt.Fprintf(response, "404 not found: %s", request.URL.Path)
		return
	}

	if request.Method == "GET" {
		buffer, err := ioutil.ReadFile("index.html")
		if err != nil {
			response.WriteHeader(500)
			fmt.Print("500 index.html is missing")
			fmt.Fprint(response, "500 index.html is missing")
			return
		}
		io.Copy(response, bytes.NewBuffer(buffer))
	} else {
		response.Header().Add("Allow", "GET")
		response.WriteHeader(405)
		fmt.Fprint(response, "405 Method Not Supported")
	}
}

func files(response http.ResponseWriter, request *http.Request) {
	var filename string
	var pathElements = strings.Split(request.RequestURI, "/")
	filename = pathElements[len(pathElements)-1]

	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		response.WriteHeader(404)
		fmt.Print("illegal file name: " + filename + "\n\n")
		fmt.Fprint(response, "illegal file name.")
		return
	}

	fullFilePath := filepath.Join(dataPath, filename)
	contentTypeFilePath := fullFilePath + ".content-type"
	if request.Method == "GET" {

		_, err := os.Stat(fullFilePath)
		if err != nil {
			response.WriteHeader(404)
			fmt.Print("404 file not found: " + fullFilePath + "\n\n")
			fmt.Fprint(response, "404 file not found")
			return
		}

		file, err := os.Open(fullFilePath)
		if err != nil {
			response.WriteHeader(500)
			fmt.Printf("500 error opening file: " + fullFilePath + " %s \n\n", err)
			fmt.Fprint(response, "500 error opening file")
			return
		}
		defer file.Close()

		contentTypeBytes, err := ioutil.ReadFile(contentTypeFilePath)
		contentType := "text/plain"
		if err == nil && string(contentTypeBytes) != "" {
			contentType = string(contentTypeBytes)
		}

		response.Header().Add("Content-Type", contentType)
		io.Copy(response, file)
	} else if request.Method == "POST" {
		_, requestPassword, _ := request.BasicAuth()
		if secretPassword != "" && requestPassword != secretPassword {
			 http.Error(response, "Unauthorized.", 401)
		} else {
			_, err := os.Stat(fullFilePath)
			if err == nil {
				response.WriteHeader(400)
				fmt.Print("400 bad request: " + fullFilePath + " already exists. \n\n")
				fmt.Fprint(response, "400 bad request: a file named \"" + filename + "\" already exists.")
				return
			}

			file, err := os.Create(fullFilePath)
			if err != nil {
				response.WriteHeader(500)
				fmt.Printf("500 error opening file: " + fullFilePath + " %s \n\n", err)
				fmt.Fprint(response, "500 error opening file")
				return
			}
			defer file.Close()

			if request.Header.Get("Content-Type") != "" {
				err = ioutil.WriteFile(contentTypeFilePath, []byte(request.Header.Get("Content-Type")), 0644)
				if err != nil {
					response.WriteHeader(500)
					fmt.Fprintf(response, "500 %s", err)
					return
				}
			}

			io.Copy(file, request.Body)

		}
	} else if request.Method == "DELETE" {
		response.WriteHeader(500)
		fmt.Fprint(response, "500 not implemented yet")
	} else {
		response.Header().Add("Allow", "GET, POST, DELETE")
		response.WriteHeader(405)
		fmt.Fprint(response, "405 Method Not Supported")
	}
}

func main() {

	secretPassword = os.ExpandEnv("$PICO_PUBLISH_PASSWORD")

	dataPath = filepath.Join(".", "data")
	os.MkdirAll(dataPath, os.ModePerm)

	http.HandleFunc("/files/", files)

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))

	http.HandleFunc("/", indexHtml)
	http.ListenAndServe(":8080", nil)
}
