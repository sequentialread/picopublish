package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
			fmt.Printf("500 error opening file: "+fullFilePath+" %s \n\n", err)
			fmt.Fprint(response, "500 error opening file")
			return
		}
		defer file.Close()

		fileStat, err := file.Stat()
		if err != nil {
			response.WriteHeader(500)
			fmt.Printf("500 error stat()-ing file: "+fullFilePath+" %s \n\n", err)
			fmt.Fprint(response, "500 error stat()-ing file")
			return
		}

		contentTypeBytes, err := ioutil.ReadFile(contentTypeFilePath)
		contentType := "text/plain"
		nameSplitOnPeriod := strings.Split(fullFilePath, ".")
		if err == nil && string(contentTypeBytes) != "" {
			contentType = string(contentTypeBytes)
		} else if len(nameSplitOnPeriod) > 1 {
			byExtension := mime.TypeByExtension(fmt.Sprintf(".%s", nameSplitOnPeriod[len(nameSplitOnPeriod)-1]))
			if byExtension != "" {
				byExtensionSplitOnSemicolon := strings.Split(byExtension, ";")
				if len(byExtensionSplitOnSemicolon) > 1 {
					byExtension = byExtensionSplitOnSemicolon[0]
				}
				contentType = byExtension
			}
		}

		response.Header().Add("Content-Type", contentType)
		response.Header().Add("Content-Length", strconv.Itoa(int(fileStat.Size())))
		io.Copy(response, file)
	} else if request.Method == "POST" {
		_, requestPassword, _ := request.BasicAuth()
		if secretPassword != "" && requestPassword != secretPassword {
			http.Error(response, "Unauthorized.", 401)
		} else {
			if request.Header.Get("X-Extract-Archive") == "true" {
				bytez, err := ioutil.ReadAll(request.Body)
				if err == nil {
					response.WriteHeader(500)
					fmt.Printf("500 bad request: error reading request body: %s \n\n", err)
					fmt.Fprint(response, "500 error reading request body.")
					return
				}

				byteReader := bytes.NewReader(bytez)
				readerAt := io.NewSectionReader(byteReader, 0, int64(len(bytez)))
				zipReader, err := zip.NewReader(readerAt, int64(len(bytez)))
				if err != nil {
					response.WriteHeader(400)
					fmt.Printf("400 error reading zip file: %s \n\n", err)
					fmt.Fprint(response, "400 error reading zip file")
					return
				}

				err = Unzip(zipReader, fullFilePath)
				if err == nil {
					response.WriteHeader(400)
					fmt.Printf("400 bad request: error reading zip file: %s \n\n", err)
					fmt.Fprint(response, "400 bad request: error reading zip file.")
					return
				}

			} else {
				_, err := os.Stat(fullFilePath)
				if err == nil {
					response.WriteHeader(400)
					fmt.Print("400 bad request: " + fullFilePath + " already exists. \n\n")
					fmt.Fprint(response, "400 bad request: a file named \""+filename+"\" already exists.")
					return
				}

				file, err := os.Create(fullFilePath)
				if err != nil {
					response.WriteHeader(500)
					fmt.Printf("500 error opening file: "+fullFilePath+" %s \n\n", err)
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

func Unzip(reader *zip.Reader, dest string) error {

	os.MkdirAll(dest, 0755)
	//fmt.Printf("os.MkdirAll(%s, 0755)\n", dest)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			//fmt.Printf("os.MkdirAll(%s, %o)\n", path, f.Mode())
		} else {
			//fmt.Printf("os.MkdirAll(%s, %o)\n", filepath.Dir(path), f.Mode())
			os.MkdirAll(filepath.Dir(path), f.Mode())
			//fmt.Printf("os.OpenFile(%s, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, %o)\n", path, f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range reader.File {
		if strings.Contains(f.Name, "..") || strings.Contains(f.Name, "\\") {
			return fmt.Errorf("Bad or wierd file name inside zip: %s", f.Name)
		}
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}
