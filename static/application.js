'use strict';

window.picoPublish = {};

(function(app, document, window, undefined){
  app.modalService = new (function ModalService() {

    var modalIsOpen = false;
    var enterKeyAction;
    var escapeKeyAction;
    var KEYCODE_ESCAPE = 27;
    var KEYCODE_ENTER = 13;

    window.addEventListener("keydown", (event) => {
      if(event.keyCode == KEYCODE_ENTER && enterKeyAction) {
        enterKeyAction();
      }
      if(event.keyCode == KEYCODE_ESCAPE && escapeKeyAction) {
        escapeKeyAction();
      }
    }, false);

    this.open = (title, body, controller, buttons) => {
      return new Promise((resolve, reject) => {
        modalIsOpen = true;
        document.getElementById('modal-container').style.display = 'block';
        document.getElementById('modal-title').innerHTML = title;
        document.getElementById('modal-body').innerHTML = body;
        var footer = document.getElementById('modal-footer');

        var closeModal = () => {
          modalIsOpen = false;
          enterKeyAction = null;
          escapeKeyAction = null;
          document.getElementById('modal-container').style.display = 'none';
          footer.innerHTML = '';
        };

        var buttonResolve = (arg) => {
          closeModal();
          resolve(arg);
        };
        var buttonReject = (arg) => {
          closeModal();
          reject(arg);
        };

        buttons.reverse();
        buttons.forEach(button => {
          var buttonElement = document.createElement("button");
          if(button.id) {
            buttonElement.id = button.id;
          }
          buttonElement.style.float = "right";
          buttonElement.innerHTML = button.innerHTML;
          var clickAction = () => {
            if(!buttonElement.disabled) {
              button.onclick(buttonResolve, buttonReject);
            }
          };
          buttonElement.onclick = clickAction;
          if(button.enterKey) {
            enterKeyAction = clickAction;
          }
          if(button.escapeKey) {
            escapeKeyAction = clickAction;
          }
          footer.appendChild(buttonElement);
        });

        controller(buttonResolve, buttonReject);
      });
    };

  })();
})(window.picoPublish, document, window);


(function(app, window, document, undefined){
  app.filePoster = new (function FilePoster(modalService) {

    var baseUrl = "/files";
    var timeoutMilliseconds = 120000;

    var requestsCurrentlyInFlight = 0;

    var request = (method, url, headers, file) =>
      new Promise((resolve, reject) => {

        requestsCurrentlyInFlight += 1;
        document.getElementById('progress-container').style.display = 'block';

        var resolveAndPopInFlight = (result) => {
          requestsCurrentlyInFlight -= 1;
          if(requestsCurrentlyInFlight == 0) {
            document.getElementById('progress-container').style.display = 'none';
          }
          resolve(result);
        };
        var rejectAndPopInFlight = (result) => {
          requestsCurrentlyInFlight -= 1;
          if(requestsCurrentlyInFlight == 0) {
            document.getElementById('progress-container').style.display = 'none';
          }
          reject(result);
        };

        headers = headers || {};
        var httpRequest = new XMLHttpRequest();
        httpRequest.onloadend = () => {
          if (httpRequest.status === 200) {
            if(httpRequest.responseText.length == 0) {
              resolveAndPopInFlight("");
            } else {
              resolveAndPopInFlight(httpRequest.responseText);
            }
          } else {
            rejectAndPopInFlight(httpRequest.responseText);
          }
        };
        //httpRequest.onerror = () => {
        //  console.log(`httpRequest.onerror: ${httpRequest.status} ${url}`);
        //  resolveAndPopInFlight(new RequestFailure(httpRequest, false));
        //};
        httpRequest.ontimeout = () => {
          //console.log(`httpRequest.ontimeout: ${httpRequest.status} ${url}`);
          rejectAndPopInFlight('HTTP Request timed out.');
        };

        httpRequest.open(method, url);
        httpRequest.timeout = timeoutMilliseconds;

        Object.keys(headers)
          .filter(key => key.toLowerCase() != 'host' && key.toLowerCase() != 'content-length')
          .forEach(key => httpRequest.setRequestHeader(key, headers[key]));

        if(file) {
          var fileReader = new FileReader();
          fileReader.readAsArrayBuffer(file);
          fileReader.onload = function(e) {
             httpRequest.send(e.target.result);
          }
        } else {
          httpRequest.send();
        }
      });


    this.post = (fileName, headers, file) => {
      return request('POST', `${baseUrl}/${fileName}`, headers, file)
    };

  })(app.modalService);
})(window.picoPublish, window, document);

(function(app, window, undefined){
  app.errorHandler = new (function ErrorHandler(modalService) {

    this.errorContent = "";

    this.onError = (message, fileName, lineNumber, column, err) => {

      this.errorContent += `<p>${message || err.message} at ${fileName || ""}:${lineNumber || ""}</p>`;
      console.log(message, fileName, lineNumber, column, err);
      document.getElementById('progress-container').style.display = 'none';
      modalService.open(
        "JavaScript Error",
        `
        ${this.errorContent}
        `,
        (resolve, reject) => {},
        [{
          innerHTML: "Ok",
          enterKey: true,
          escapeKey: true,
          onclick: (resolve, reject) => resolve()
        }]
      ).then(() => {
        this.errorContent = "";
      });
    };

    window.onerror = this.onError;
    window.addEventListener("unhandledrejection", (unhandledPromiseRejectionEvent, promise) => {
      var err = unhandledPromiseRejectionEvent.reason;
      if(typeof err == "string") {
        err = new Error(err);
      }
      if(err) {
        this.onError(err.message, err.fileName, err.lineNumber, null, err);
      }
    });
  })(app.modalService);
})(window.picoPublish, window);





(function(app, window, document, undefined){
  app.mainController = new (function MainContrller(modalService, filePoster) {

    var fileInput = document.getElementById("file");
    var filenameInput = document.getElementById("filename");
    var contentTypeInput = document.getElementById("content-type");
    var uploadButton = document.getElementById("upload-button");

    var extractArchive = document.getElementById("extract-archive");
    var extractArchiveContainer = document.getElementById("extract-archive-container");

    var handleContentTypeChanged = () => {
      const isArchive = contentTypeInput.value == 'application/zip';
      extractArchive.checked = extractArchive.checked && isArchive;
      extractArchiveContainer.style.display = isArchive ? 'block' : 'none';
    };

    fileInput.onchange = () => {
      filenameInput.value = fileInput.files[0].name;
      contentTypeInput.value = fileInput.files[0].type;
      handleContentTypeChanged();
    };

    contentTypeInput.onchange = () => handleContentTypeChanged();
    contentTypeInput.onkeyup = () => handleContentTypeChanged();

    uploadButton.addEventListener('click', () => {


      if(fileInput.files.length == 0 || fileInput.files.length > 1) {
        modalService.open(
          "Unsupported Operation",
          "Zero files selected or more than one file selected. This is not supported.",
          (resolve, reject) => {},
          [{
            innerHTML: "Ok",
            enterKey: true,
            escapeKey: true,
            onclick: (resolve, reject) => resolve()
          }]
        );
      } else {
        const password = window.localStorage.getItem('pico-publish-password');
        let getPasswordPromise;
        if((password || '') != '') {
          getPasswordPromise = Promise.resolve(password);
        } else {
          getPasswordPromise = modalService.open(
            "Password",
            `
            <input type="password" id="password"></input>
            `,
            (resolve, reject) => {
              document.getElementById("password").focus();
            },
            [{
              innerHTML: "Cancel",
              escapeKey: true,
              onclick: (resolve, reject) => reject()
            },
            {
              innerHTML: "Ok",
              enterKey: true,
              onclick: (resolve, reject) => {
                resolve(document.getElementById("password").value);
              }
            }]
          );
        }

        getPasswordPromise.then((password) => {
          const postFileHeaders = {
            'X-Extract-Archive': extractArchive.checked ? "true" : "false",
            'Content-Type': contentTypeInput.value, 
            'Authorization': `Basic ${btoa(`admin:${password}`)}`
          };
          filePoster.post(filenameInput.value, postFileHeaders, fileInput.files[0])
            .then((responseText) => {
                window.localStorage.setItem('pico-publish-password', password);
                var linkPath = filenameInput.value;
                if(extractArchive.checked) {
                  linkPath = `${filenameInput.value}/${filenameInput.value.replace('.zip', '')}`
                }
                modalService.open(
                  "Success",
                  `<a href="${window.location}files/${filenameInput.value}">${window.location}files/${filenameInput.value}</a>`,
                  (resolve, reject) => {},
                  [{
                    innerHTML: "Ok",
                    enterKey: true,
                    onclick: (resolve, reject) => resolve()
                  }]
                );
              },
              (responseText) => {
                if(responseText.toLowerCase().includes('unauthorized')) {
                  window.localStorage.setItem('pico-publish-password', '');
                }
                modalService.open(
                  "Failure",
                  responseText,
                  (resolve, reject) => {},
                  [{
                    innerHTML: "Ok",
                    enterKey: true,
                    onclick: (resolve, reject) => resolve()
                  }]
                );
              },
            )
        });
      }
    });


  })(app.modalService, app.filePoster);
})(window.picoPublish, window, document);
