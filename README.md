`github-backup` is a WIP PaaS-ready implementation of a worker that periodically
backs up a list of repositories by cloning them (using an SSH key) and pushing
TAR archives to a specified FTP server.

Technically, repos hosted elsewhere than GitHub are supported.
