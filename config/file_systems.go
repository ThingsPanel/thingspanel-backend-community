package config

func NewFileSystemConfig(appURL string) *FileSystemsConfig {
	fsc := &FileSystemsConfig{}
	fsc.Disks.Local.Driver = `local`
	fsc.Disks.Local.Root = `storage/app`
	fsc.Disks.Extension.Driver = `local`
	fsc.Disks.Extension.Root = `app/Extensions`
	fsc.Disks.Public.Driver = `local`
	fsc.Disks.Public.Root = `storage/app/public`
	fsc.Disks.Public.URL = appURL + "/storage"
	fsc.Disks.Public.Visibility = `public`
	fsc.Disks.S3.Driver = `s3`

	return fsc
}

type FileSystemsConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | Default Filesystem Disk
	   |--------------------------------------------------------------------------
	   |
	   | Here you may specify the default filesystem disk that should be used
	   | by the framework. The "local" disk, as well as a variety of cloud
	   | based disks are available to your application. Just store away!
	   |
	*/
	Default string `json:"default" env:"FILESYSTEM_DRIVER" envDefault:"local"`
	/*
	   |--------------------------------------------------------------------------
	   | Default Cloud Filesystem Disk
	   |--------------------------------------------------------------------------
	   |
	   | Many applications store files both locally and in the cloud. For this
	   | reason, you may specify a default "cloud" driver here. This driver
	   | will be bound as the Cloud disk implementation in the container.
	   |
	*/
	Cloud string `json:"cloud" env:"FILESYSTEM_CLOUD" envDefault:"s3"`
	/*
	   |--------------------------------------------------------------------------
	   | Filesystem Disks
	   |--------------------------------------------------------------------------
	   |
	   | Here you may configure as many filesystem "disks" as you wish, and you
	   | may even configure multiple disks of the same driver. Defaults have
	   | been setup for each driver as an example of the required options.
	   |
	   | Supported Drivers: "local", "ftp", "sftp", "s3"
	   |
	*/
	Disks struct {
		Local struct {
			Driver string `json:"driver"`
			Root   string `json:"root"`
		} `json:"local"`
		Extension struct {
			Driver string `json:"driver"`
			Root   string `json:"root"`
		} `json:"extension"`
		Public struct {
			Driver     string `json:"driver"`
			Root       string `json:"root"`
			URL        string `json:"url" env:"APP_URL" envDefault:"/storage"`
			Visibility string `json:"visibility"`
		} `json:"public"`
		S3 struct {
			Driver string `json:"driver"`
			Key    string `json:"key" env:"AWS_ACCESS_KEY_ID"`
			Secret string `json:"secret" env:"AWS_SECRET_ACCESS_KEY"`
			Region string `json:"region" env:"AWS_DEFAULT_REGION"`
			Bucket string `json:"bucket" env:"AWS_BUCKET"`
			URL    string `json:"url" env:"AWS_URL"`
		} `json:"s3"`
	} `json:"disks"`
}
