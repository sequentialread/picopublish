## 📤📚 picopublish

I created this application as a self-hosted replacement for quick-and-dirty image hosting services like imgur.

I use it to host images, video files, Unity3d Games (unity3d HTML/WebGL builds), etc.

It supports uploading one file at a time, or uploading a zip file and extracting it.

There is no "list files" endpoint. So long/unpredictable file names can be considered "private to anyone who has access to the link".

Uploading files requires entering a password. The password is saved in LocalStorage of your browser after you enter it the first time.

Optionally supports [💥PoW! Captcha](https://git.sequentialread.com/forest/pow-captcha) to prevent bots from scraping the uploaded files. 

