# photouploader

Include `asset-bucket-sa-key.json` in local install to connect.

How to run:
```
go mod download
go build -o ./photouploader-server
./photouploader-server
```

Running in Docker container:
```
docker build -t photouploader .
docker run -p 80:80 -d photouploader
```
