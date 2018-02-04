## webclip

The easy way to get a file off of a remote machine without using `scp`.

Usage:

```
me@my-desktop:~$ ssh foo@fooserver.io
foo@fooserver:~$ cd /my/stuff
foo@fooserver:/my/stuff$ ls -lah
drwxrwxr-x 2 foo foo 4.0K  Nov  6 18:48 .
drwxrwxr-x 3 foo foo 4.0K  Nov  6 18:48 ..
-rw-rw-r-- 1 foo foo 10.0M Nov  6 18:48 myArchive.gz
-rw-rw-r-- 1 foo foo 28.0K Nov  6 18:48 myFile.txt
foo@fooserver:/my/stuff$ curl -s https://webclip.mydomain.com/myArchive.gz | bash
200 ok I got "myArchive.gz" with 10000000 bytes.
foo@fooserver:/my/stuff$ exit
me@my-desktop:~$ curl -s https://webclip.mydomain.com > myArchive.gz
```

After you clip something you may also grab it by hitting https://webclip.mydomain.com in your web browser.

Once it is downloaded the first time, it will be removed from the server and cannot be downloaded again.

Only one file can be clipped at once. 
