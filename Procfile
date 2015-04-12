web: frontend -listen 0.0.0.0:5000 -static dist -id $GITHUB_APP_ID -secret $GITHUB_APP_SECRET -public $PUBLIC_URL -redis $REDIS_URL
worker: downloader -ftp $FTP_URL -redis $REDIS_URL $DOWNLOADER_EXTRA_PARAMS
