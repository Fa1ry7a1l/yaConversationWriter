package telegram

type Message struct {
	ID    int64
	Chat  Chat
	From  *User
	Text  string
	Voice *VoiceFile
	Audio *AudioFile
	File  *DocumentFile
}

type Chat struct {
	ID   int64
	Type string
}

type User struct {
	ID    int64
	IsBot bool
}

type VoiceFile struct {
	FileID   string
	Duration int
	MIMEType string
	FileSize int64
}

type AudioFile struct {
	FileID   string
	FileName string
	Duration int
	MIMEType string
	FileSize int64
}

type DocumentFile struct {
	FileID   string
	FileName string
	MIMEType string
	FileSize int64
}
