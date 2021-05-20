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

	errors "git.sequentialread.com/forest/pkg-errors"
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
	filename := strings.Replace(request.RequestURI, "files/", "", 1)

	if strings.Contains(filename, "..") || strings.Contains(filename, "\\") {
		response.WriteHeader(404)
		fmt.Print("illegal file name: " + filename + "\n\n")
		fmt.Fprint(response, "illegal file name.")
		return
	}

	fullFilePath := filepath.Join(dataPath, filename)
	contentTypeFilePath := fullFilePath + ".content-type"
	if request.Method == "GET" {

		stat, err := os.Stat(fullFilePath)
		if err != nil {
			response.WriteHeader(404)
			fmt.Print("404 file not found: " + fullFilePath + "\n\n")
			fmt.Fprint(response, "404 file not found")
			return
		}

		if stat.IsDir() {
			_, err := os.Stat(fullFilePath + "/index.html")
			if err == nil {
				response.Header().Add("Location", request.RequestURI+"/index.html")
				response.WriteHeader(302)
			} else {
				response.WriteHeader(404)
				fmt.Printf("404 file not found: %s (dir) \n\n", fullFilePath)
				fmt.Fprint(response, "404 file not found")
			}
		}

		file, err := os.Open(fullFilePath)
		if err != nil {
			response.WriteHeader(500)
			fmt.Printf("500 error opening file: %s %s \n\n", fullFilePath, err)
			fmt.Fprint(response, "500 error opening file")
			return
		}
		defer file.Close()

		fileStat, err := file.Stat()
		if err != nil {
			response.WriteHeader(500)
			fmt.Printf("500 error stat()-ing file: %s %s \n\n", fullFilePath, err)
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

		response.Header().Add("accept-ranges", "bytes")
		response.Header().Add("Content-Type", contentType)

		if strings.HasPrefix(request.Header.Get("Range"), "bytes=") {
			bytesSpecString := strings.TrimPrefix(request.Header.Get("Range"), "bytes=")

			startByte := int64(0)
			endByte := int64(-1)
			var err error
			if strings.HasSuffix(bytesSpecString, "-") {
				startByte, err = strconv.ParseInt(bytesSpecString[:len(bytesSpecString)-1], 10, 64)
			} else if strings.HasPrefix(bytesSpecString, "-") {
				endByte, err = strconv.ParseInt(bytesSpecString[1:], 10, 64)
			} else {
				numbers := strings.Split(bytesSpecString, "-")
				if len(numbers) != 2 {
					err = errors.New("expected two numbers separated by a hyphen")
				} else {
					startByte, err = strconv.ParseInt(numbers[0], 10, 64)
					if err == nil {
						endByte, err = strconv.ParseInt(numbers[1], 10, 64)
					}
				}
			}
			if endByte == -1 {
				endByte = fileStat.Size() - 1
			}
			if startByte > endByte {
				err = errors.New("startByte > endByte")
			}
			if endByte > fileStat.Size() {
				err = errors.New("endByte > fileStat.Size()")
			}

			if err != nil {
				response.WriteHeader(400)
				fmt.Printf("400 bad request: bad range header format: '%s': %s \n\n", request.Header.Get("Range"), err)
				fmt.Fprint(response, "400 bad request: bad Range header format")
				return
			}

			response.Header().Add("Content-Range", fmt.Sprintf("bytes %d-%d/%d", startByte, endByte, fileStat.Size()))
			response.Header().Add("Content-Length", strconv.FormatInt(endByte-startByte, 10))
			sectionReader := io.NewSectionReader(file, startByte, endByte-startByte)
			io.Copy(response, sectionReader)
		} else {
			response.Header().Add("Content-Length", strconv.FormatInt(fileStat.Size(), 10))
			io.Copy(response, file)
		}

	} else if request.Method == "POST" {
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
				if err != nil {
					response.WriteHeader(400)
					fmt.Printf("400 bad request: error expanding zip file: %s \n\n", err)
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

	destSplit := strings.Split(dest, "/")

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

		fileName := f.Name
		rootFolderName := destSplit[len(destSplit)-1] + "/"
		if strings.HasPrefix(fileName, rootFolderName) {
			fileName = fileName[len(rootFolderName):]
		}

		path := filepath.Join(dest, fileName)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			//fmt.Printf("os.MkdirAll(%s, %o)\n", path, f.Mode())
		} else {
			//fmt.Printf("os.MkdirAll(%s, %o)\n", filepath.Dir(path), f.Mode())
			os.MkdirAll(filepath.Dir(path), f.Mode())
			//fmt.Printf("os.OpenFile(%s, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, %o)\n", path, f.Mode())
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

			contentType, err := GetFileContentType(f)
			if err != nil {
				return errors.Wrapf(err, "cant get content type for '%s'", fileName)
			}
			contentTypeFilePath := path + ".content-type"
			err = ioutil.WriteFile(contentTypeFilePath, []byte(contentType), 0644)
			if err != nil {
				return errors.Wrap(err, "cant write content type file")
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

// https://golangcode.com/get-the-content-type-of-file/
func GetFileContentType(out *os.File) (string, error) {

	// Only the first 512 bytes are used to sniff the content type.
	buffer := make([]byte, 512)

	_, err := out.Read(buffer)
	if err != nil {
		return "", err
	}

	contentType := http.DetectContentType(buffer)

	return contentType, nil
}
