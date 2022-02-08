run:
	go run .

build:
	go build .
	rm -rf nerved.app
	mkdir -p nerved.app/Contents/MacOS
	mv nerved nerved.app/Contents/MacOS
	mkdir -p nerved.app/Contents/Resources
	cp support/icon.icns nerved.app/Contents/Resources
	cp support/info.plist nerved.app/Contents/Info.plist
