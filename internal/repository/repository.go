package repository

import "github.com/NamanBalaji/tdm/internal/downloader"

type Repository interface {
	Save(download *downloader.Download) error
	Find(id string) (*downloader.Download, error)
	FindAll() ([]*downloader.Download, error)
	Delete(id string) error
}
