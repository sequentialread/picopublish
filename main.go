package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	errors "git.sequentialread.com/forest/pkg-errors"
)

var secretPassword = ""
var dataPath = ""
var readFileHandler http.Handler

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
	if request.Method == "GET" {
		readFileHandler.ServeHTTP(response, request)
	} else if request.Method == "POST" {
		filename := strings.Replace(request.RequestURI, "files/", "", 1)

		if strings.Contains(filename, "..") || strings.Contains(filename, "\\") {
			response.WriteHeader(404)
			fmt.Print("illegal file name: " + filename + "\n\n")
			fmt.Fprint(response, "illegal file name.")
			return
		}

		fullFilePath := filepath.Join(dataPath, filename)

		_, requestPassword, _ := request.BasicAuth()
		if secretPassword != "" && requestPassword != secretPassword {
			http.Error(response, "Unauthorized.", 401)
		} else {
			if request.Header.Get("X-Extract-Archive") == "true" {

				if strings.HasSuffix(fullFilePath, ".zip") {
					fullFilePath = fullFilePath[0 : len(fullFilePath)-len(".zip")]
				}

				_, err := os.Stat(fullFilePath)
				if err == nil {
					response.WriteHeader(400)
					fmt.Print("400 bad request: " + fullFilePath + " already exists. \n\n")
					fmt.Fprint(response, "400 bad request: a file named \""+filename+"\" already exists.")
					return
				}

				bytez, err := ioutil.ReadAll(request.Body)
				if err != nil {
					response.WriteHeader(500)
					log.Printf("500 bad request: error reading request body: %s \n\n", err)
					fmt.Fprint(response, "500 error reading request body.")
					return
				}

				// log.Printf("Zip upload: len(bytez) = %d  \n", len(bytez))
				// logBytez := bytez
				// if len(logBytez) > 1000 {
				// 	logBytez = logBytez[0:1000]
				// }
				// log.Printf("Zip upload: bytez = %s  \n", string(logBytez))

				byteReader := bytes.NewReader(bytez)
				readerAt := io.NewSectionReader(byteReader, 0, int64(len(bytez)))
				zipReader, err := zip.NewReader(readerAt, int64(len(bytez)))
				if err != nil {
					response.WriteHeader(400)
					log.Printf("400 error reading zip file: %s \n\n", err)
					fmt.Fprint(response, "400 error reading zip file")
					return
				}

				err = Unzip(zipReader, fullFilePath)
				if err != nil {
					response.WriteHeader(400)
					log.Printf("400 bad request: error expanding zip file: %s \n\n", err)
					fmt.Fprint(response, "400 bad request: error expanding zip file.")
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
					log.Printf("500 error opening file: "+fullFilePath+" %s \n\n", err)
					fmt.Fprint(response, "500 error opening file")
					return
				}
				defer file.Close()

				// if request.Header.Get("Content-Type") != "" {
				// 	err = ioutil.WriteFile(contentTypeFilePath, []byte(request.Header.Get("Content-Type")), 0644)
				// 	if err != nil {
				// 		response.WriteHeader(500)
				// 		fmt.Fprintf(response, "500 %s", err)
				// 		return
				// 	}
				// }

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

	// https://stackoverflow.com/questions/49589685/good-way-to-disable-directory-listing-with-http-fileserver-in-go
	noDirectoryListingHTTPDir := justFilesFilesystem{fs: http.Dir(dataPath), readDirBatchSize: 20}
	readFileHandler = http.StripPrefix("/files/", http.FileServer(noDirectoryListingHTTPDir))

	http.HandleFunc("/files/", files)

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))

	http.HandleFunc("/", indexHtml)
	http.ListenAndServe(":8080", nil)
}

func Unzip(reader *zip.Reader, dest string) error {

	destSplit := strings.Split(dest, "/")

	os.MkdirAll(dest, 0755)
	//log.Printf("os.MkdirAll(%s, 0755)\n", dest)

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

		fileName := f.Name
		rootFolderName := destSplit[len(destSplit)-1] + "/"
		if strings.HasPrefix(fileName, rootFolderName) {
			fileName = fileName[len(rootFolderName):]
		}

		path := filepath.Join(dest, fileName)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			//log.Printf("os.MkdirAll(%s, %o)\n", path, f.Mode())
		} else {
			//log.Printf("os.MkdirAll(%s, %o)\n", filepath.Dir(path), f.Mode())
			os.MkdirAll(filepath.Dir(path), f.Mode())
			//log.Printf("os.OpenFile(%s, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, %o)\n", path, f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return errors.Wrapf(err, "cant open file '%s'", fileName)
			}
			defer func() {
				f.Close()
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return errors.Wrapf(err, "cant write file '%s'", fileName)
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

// if strings.HasSuffix(fullFilePath, ".css") {
// 	contentType = "text/css"
// }
// if strings.HasSuffix(fullFilePath, ".svg") {
// 	contentType = "image/svg"
// }

// https://golangcode.com/get-the-content-type-of-file/
func GetFileContentType(filename string, out *os.File) (string, error) {

	// Only the first 512 bytes are used to sniff the content type.
	buffer := make([]byte, 512)

	_, err := out.Read(buffer)
	if err != nil {
		return "", err
	}

	contentType := http.DetectContentType(buffer)

	_, err = out.Seek(0, 0)
	if err != nil {
		return "", err
	}

	if strings.Contains(filename, ".") && (strings.Contains(contentType, "text/plain") || strings.Contains(contentType, "octet-stream")) {
		splitOnPeriod := strings.Split(filename, ".")
		return mime.TypeByExtension(fmt.Sprintf(".%s", splitOnPeriod[len(splitOnPeriod)-1])), nil
	}

	return contentType, nil
}

type justFilesFilesystem struct {
	fs http.FileSystem
	// readDirBatchSize - configuration parameter for `Readdir` func
	readDirBatchSize int
}

func (fs justFilesFilesystem) Open(name string) (http.File, error) {
	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return neuteredStatFile{File: f, readDirBatchSize: fs.readDirBatchSize}, nil
}

type neuteredStatFile struct {
	http.File
	readDirBatchSize int
}

func (e neuteredStatFile) Stat() (os.FileInfo, error) {
	s, err := e.File.Stat()
	if err != nil {
		return nil, err
	}
	if s.IsDir() {
	LOOP:
		for {
			fl, err := e.File.Readdir(e.readDirBatchSize)
			switch err {
			case io.EOF:
				break LOOP
			case nil:
				for _, f := range fl {
					if f.Name() == "index.html" {
						return s, err
					}
				}
			default:
				return nil, err
			}
		}
		return nil, os.ErrNotExist
	}
	return s, err
}
