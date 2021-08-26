package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	errors "git.sequentialread.com/forest/pkg-errors"
	"github.com/shengdoushi/base58"
)

var secretPassword = ""
var captchaAPIToken = ""
var dataPath = ""
var readFileHandler http.Handler

var httpClient *http.Client
var captchaAPIURL *url.URL
var captchaPublicURL *url.URL
var captchaChallenges []string
var loadCaptchaChallengesMutex *sync.Mutex
var captchaChallengesMutex *sync.Mutex
var loadCaptchaChallengesMutexIsProbablyLocked = false

var disallowBotsToken map[string]SolvedDisallowBotsChallenge
var matchDisallowBotsToken *regexp.Regexp

const captchaDifficultyLevel = 5

type SolvedDisallowBotsChallenge struct {
	IdentityHash string
	Time         time.Time
}

func main() {

	secretPassword = os.ExpandEnv("$PICOPUBLISH_PASSWORD")
	if secretPassword == "" {
		panic(errors.New("can't start the app, the PICOPUBLISH_PASSWORD environment variable is required"))
	}

	captchaAPIToken = os.ExpandEnv("$PICOPUBLISH_CAPTCHA_API_TOKEN")
	if captchaAPIToken == "" {
		log.Println("the CAPTCHA_API_TOKEN environment variable is not set, the captcha feature will not work.")
	}

	captchaAPIURLString := os.ExpandEnv("$PICOPUBLISH_CAPTCHA_API_URL")
	if captchaAPIURLString == "" {
		captchaAPIURLString = "http://localhost:2370"
	}
	var err error
	captchaAPIURL, err = url.Parse(captchaAPIURLString)
	if err != nil {
		panic(errors.New("can't start the app because can't parse PICOPUBLISH_CAPTCHA_API_URL"))
	}

	captchaPublicURLString := os.ExpandEnv("$PICOPUBLISH_CAPTCHA_PUBLIC_URL")
	if captchaPublicURLString == "" {
		captchaPublicURLString = "https://captcha.sequentialread.com"
	}
	captchaPublicURL, err = url.Parse(captchaPublicURLString)
	if err != nil {
		panic(errors.New("can't start the app because can't parse PICOPUBLISH_CAPTCHA_PUBLIC_URL"))
	}

	disallowBotsToken = map[string]SolvedDisallowBotsChallenge{}
	httpClient = &http.Client{
		Timeout: time.Second * time.Duration(5),
	}
	loadCaptchaChallengesMutex = &sync.Mutex{}
	captchaChallengesMutex = &sync.Mutex{}

	dataPath = filepath.Join(".", "data")
	os.MkdirAll(dataPath, os.ModePerm)

	matchDisallowBotsToken = regexp.MustCompile("^[a-zA-Z0-9]{8}$")

	// https://stackoverflow.com/questions/49589685/good-way-to-disable-directory-listing-with-http-fileserver-in-go
	noDirectoryListingHTTPDir := justFilesFilesystem{fs: http.Dir(dataPath), readDirBatchSize: 20}
	readFileHandler = http.FileServer(noDirectoryListingHTTPDir)

	http.HandleFunc("/files/", files)

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))

	http.HandleFunc("/", indexHtml)

	log.Println("📤📚 PicoPublish is about to start listening on port 8080 ")

	err = http.ListenAndServe(":8080", nil)
	panic(err)
}

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
	fileFirstPathElement := filterStringsNonEmpty(strings.Split(filename, "/"))[0]

	if request.Method == "GET" {

		// if the $upload_name.disallowbots  file exists, then we redirect to the disallow bots handler with a random token
		disallowBotsFlagPath := filepath.Join(dataPath, fmt.Sprintf("%s.disallowbots", fileFirstPathElement))
		_, err := os.Stat(disallowBotsFlagPath)
		if err == nil {
			http.Redirect(response, request, strings.Replace(request.RequestURI, "files/", fmt.Sprintf("files/%s/", getNewToken()), 1), 302)
			return
		}

		// If this happens, either we just got redirected, or we just solved the captcha challenge, or someone copy and pasted a link
		// to a solved challenge.
		if matchDisallowBotsToken.MatchString(fileFirstPathElement) {
			solved, hasSolved := disallowBotsToken[fileFirstPathElement]

			// If the captcha challenge has been solved, then
			if hasSolved {
				// Ensure the captcha challenge has been solved by this user within the last day. If so, serve the file.
				// Otherwise, redirect to a new challenge.
				if getIdentityHash(*request) == solved.IdentityHash && time.Since(solved.Time) < time.Hour*24 {
					http.StripPrefix(fmt.Sprintf("/files/%s/", fileFirstPathElement), readFileHandler).ServeHTTP(response, request)
					return
				} else {
					http.Redirect(response, request, strings.Replace(request.RequestURI, fileFirstPathElement, getNewToken(), 1), 302)
					return
				}
			} else {
				// if this captcha was never solved, then we can display the HTML page to solve it!

				// if it looks like we will run out of challenges soon & not currently busy getting them,
				// then kick off a goroutine to go get them in the background.
				if len(captchaChallenges) > 0 && len(captchaChallenges) < 5 && !loadCaptchaChallengesMutexIsProbablyLocked {
					go loadCaptchaChallenges(captchaAPIToken)
				}

				if captchaChallenges == nil || len(captchaChallenges) == 0 {
					err = loadCaptchaChallenges(captchaAPIToken)
					if err != nil {
						log.Printf("loading captcha challenges failed: %v\n", err)
						response.WriteHeader(500)
						response.Write([]byte("captcha api error"))
						return
					}
				}

				var challenge string
				captchaChallengesMutex.Lock()
				challenge = captchaChallenges[0]
				captchaChallenges = captchaChallenges[1:]
				captchaChallengesMutex.Unlock()

				htmlBytes, err := renderCaptchaPageTemplate(challenge)
				if err != nil {
					log.Printf("renderPageTemplate(): %+v", err)
					response.WriteHeader(500)
					response.Write([]byte("500 internal server error"))
					return
				}
				response.Header().Set("Content-Type", "text/html; charset=UTF-8")
				response.Write(htmlBytes)
				return
			}
		}

		// default: just serve the dang file :D
		http.StripPrefix("/files/", readFileHandler).ServeHTTP(response, request)

	} else if request.Method == "POST" {

		if strings.Contains(filename, "..") || strings.Contains(filename, "\\") {
			response.WriteHeader(404)
			fmt.Print("illegal file name: " + filename + "\n\n")
			fmt.Fprint(response, "illegal file name.")
			return
		}

		if matchDisallowBotsToken.MatchString(fileFirstPathElement) {
			err := request.ParseForm()
			if err == nil {
				challenge := request.Form.Get("challenge")
				nonce := request.Form.Get("nonce")
				if challenge != "" && nonce != "" {
					err = validateCaptcha(captchaAPIToken, challenge, nonce)
				} else {
					err = errors.New("challenge and nonce are required")
				}
			}
			if err != nil {
				response.WriteHeader(400)
				response.Write([]byte(fmt.Sprintf("400 bad request: %s", err)))
				return
			}
			disallowBotsToken[fileFirstPathElement] = SolvedDisallowBotsChallenge{
				IdentityHash: getIdentityHash(*request),
				Time:         time.Now(),
			}
			http.Redirect(response, request, request.RequestURI, 302)
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

				io.Copy(file, request.Body)
			}

			if request.Header.Get("X-Disallow-Bots") == "true" {
				ioutil.WriteFile(fmt.Sprintf("%s.disallowbots", fullFilePath), []byte("true"), 0644)
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

func getNewToken() string {
	randomBytesBuffer := make([]byte, 16)
	rand.Read(randomBytesBuffer)
	return base58.Encode(randomBytesBuffer, base58.BitcoinAlphabet)[0:8]
}

func getIdentityHash(request http.Request) string {

	remoteAddrString := ""
	parsedRemoteAddr, err := net.ResolveTCPAddr("tcp", request.RemoteAddr)
	if err != nil {
		log.Printf("net.ResolveTCPAddr(\"tcp\", request.RemoteAddr): %+v", err)
	} else {
		remoteAddrString = parsedRemoteAddr.IP.String()
	}

	// log.Printf(
	// 	"\n\nresolveTCPAddr: %s, X-Forwarded-For: %s, X-Real-IP: %s\n\n",
	// 	remoteAddrString, request.Header.Get("X-Forwarded-For"), request.Header.Get("X-Real-IP"),
	// )
	if request.Header.Get("X-Forwarded-For") != "" {
		remoteAddrString = request.Header.Get("X-Forwarded-For")
	} else if request.Header.Get("X-Real-IP") != "" {
		remoteAddrString = request.Header.Get("X-Real-IP")
	}

	hashBytes := sha256.Sum256([]byte(fmt.Sprintf("%s.%s.Bzb2Z0bHkgeW9ndXJ0IG1vcnRvbiBkdW1teSByYWNrIG1vdGlv", remoteAddrString, request.UserAgent())))
	return base58.Encode(hashBytes[8:16], base58.BitcoinAlphabet)
}

func filterStringsNonEmpty(input []string) []string {
	toReturn := []string{}
	for _, s := range input {
		if strings.TrimSpace(s) != "" {
			toReturn = append(toReturn, s)
		}
	}
	return toReturn
}

func renderCaptchaPageTemplate(challenge string) ([]byte, error) {

	// in a "real" application in production you would read the template file & parse it 1 time when the app starts
	// I'm doing it for each request here just to make it easier to hack on it while its running 😇
	htmlTemplateString, err := ioutil.ReadFile("disallowbots.gotemplate.html")
	if err != nil {
		return nil, errors.Wrap(err, "can't open the template file. Are you in the right directory? ")
	}
	pageTemplate, err := template.New("master").Parse(string(htmlTemplateString))
	if err != nil {
		return nil, errors.Wrap(err, "can't parse the template file: ")
	}

	// constructing an instance of an anonymous struct type to contain all the data
	// that we need to pass to the template
	pageData := struct {
		Challenge  string
		CaptchaURL string
	}{
		Challenge:  challenge,
		CaptchaURL: captchaPublicURL.String(),
	}
	var outputBuffer bytes.Buffer
	err = pageTemplate.Execute(&outputBuffer, pageData)
	if err != nil {
		return nil, errors.Wrap(err, "rendering page template failed: ")
	}

	return outputBuffer.Bytes(), nil
}

func loadCaptchaChallenges(apiToken string) error {
	// make sure we only call this function once at a time.
	loadCaptchaChallengesMutex.Lock()
	loadCaptchaChallengesMutexIsProbablyLocked = true
	defer (func() {
		loadCaptchaChallengesMutexIsProbablyLocked = false
		loadCaptchaChallengesMutex.Unlock()
	})()

	query := url.Values{}
	query.Add("difficultyLevel", strconv.Itoa(captchaDifficultyLevel))

	loadURL := url.URL{
		Scheme:   captchaAPIURL.Scheme,
		Host:     captchaAPIURL.Host,
		Path:     filepath.Join(captchaAPIURL.Path, "GetChallenges"),
		RawQuery: query.Encode(),
	}

	captchaRequest, err := http.NewRequest("POST", loadURL.String(), nil)
	captchaRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiToken))
	if err != nil {
		return err
	}

	response, err := httpClient.Do(captchaRequest)
	if err != nil {
		return err
	}

	responseBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf(
			"load proof of work captcha challenges api returned http %d: %s",
			response.StatusCode, string(responseBytes),
		)
	}

	err = json.Unmarshal(responseBytes, &captchaChallenges)
	if err != nil {
		return err
	}

	if len(captchaChallenges) == 0 {
		return errors.New("proof of work captcha challenges api returned empty array")
	}

	return nil
}

func validateCaptcha(apiToken, challenge, nonce string) error {
	query := url.Values{}
	query.Add("challenge", challenge)
	query.Add("nonce", nonce)

	verifyURL := url.URL{
		Scheme:   captchaAPIURL.Scheme,
		Host:     captchaAPIURL.Host,
		Path:     filepath.Join(captchaAPIURL.Path, "Verify"),
		RawQuery: query.Encode(),
	}

	captchaRequest, err := http.NewRequest("POST", verifyURL.String(), nil)
	captchaRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiToken))
	if err != nil {
		return err
	}

	response, err := httpClient.Do(captchaRequest)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return errors.New("proof of work captcha validation failed")
	}
	return nil
}
