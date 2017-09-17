Command `merge-docker-save` repacks output of the docker save command called for a single image to a tar stream with merged content of all image layers.

Command `docker save` outputs docker-specific tar stream:

	$ docker save alpine:latest | tar tv |head
	drwxr-xr-x 0/0               0 2017-09-13 14:32 030cf30aac0be224d6b7e02ca2b7cadf1d078bf90ef1e9656ff54a924a5f163a/
	-rw-r--r-- 0/0               3 2017-09-13 14:32 030cf30aac0be224d6b7e02ca2b7cadf1d078bf90ef1e9656ff54a924a5f163a/VERSION
	-rw-r--r-- 0/0            1184 2017-09-13 14:32 030cf30aac0be224d6b7e02ca2b7cadf1d078bf90ef1e9656ff54a924a5f163a/json
	-rw-r--r-- 0/0         4220928 2017-09-13 14:32 030cf30aac0be224d6b7e02ca2b7cadf1d078bf90ef1e9656ff54a924a5f163a/layer.tar
	-rw-r--r-- 0/0            1512 2017-09-13 14:32 76da55c8019d7a47c347c0dceb7a6591144d232a7dd616242a367b8bed18ecbc.json
	-rw-r--r-- 0/0             202 1970-01-01 00:00 manifest.json
	-rw-r--r-- 0/0              89 1970-01-01 00:00 repositories

Command `merge-docker-save` transforms this output to produce tar stream with container filesystem:

	$ docker save alpine:latest | merge-docker-save | tar tv |head
	drwxr-xr-x 0/0               0 2017-06-25 17:52 bin/
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/ash -> /bin/busybox
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/base64 -> /bin/busybox
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/bbconfig -> /bin/busybox
	-rwxr-xr-x 0/0          825504 2017-06-11 06:38 bin/busybox
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/cat -> /bin/busybox
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/catv -> /bin/busybox
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/chgrp -> /bin/busybox
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/chmod -> /bin/busybox
	lrwxrwxrwx 0/0               0 2017-06-25 17:52 bin/chown -> /bin/busybox

## Known limits

Command only supports `docker save` output for a single image, i.e. piping `docker save alpine:latest` works, but `docker save alpine` may not if it outputs multiple images.

Because this command essentially concatenates multiple tar archives one after another, some features are not supported, i.e. removal of the file from the previous layer by the next one. If you have the following Dockerfile:

	COPY requirements.pip .
	RUN pip install -r requirements.pip && rm requirements.pip

Resulting archive would still have both `requirements.pip` file and empty unreadable `.wh.requirements.pip` alongside it.

This tool uses system default directory to store temporary files, which can be overridden by setting `$TMPDIR` environment.
