package feishu

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestIMFileTypeForSemantic(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		semantic imUploadedFileSemantic
		want     string
		wantErr  string
	}{
		{
			name:     "attachment markdown falls back to stream",
			fileName: "README.md",
			semantic: imUploadedFileSemanticAttachment,
			want:     larkim.CreateFileFileTypeStream,
		},
		{
			name:     "attachment mp4 stays generic attachment",
			fileName: "demo.mp4",
			semantic: imUploadedFileSemanticAttachment,
			want:     larkim.CreateFileFileTypeStream,
		},
		{
			name:     "attachment opus stays generic attachment",
			fileName: "voice.opus",
			semantic: imUploadedFileSemanticAttachment,
			want:     larkim.CreateFileFileTypeStream,
		},
		{
			name:     "attachment docx uses doc family",
			fileName: "proposal.docx",
			semantic: imUploadedFileSemanticAttachment,
			want:     larkim.CreateFileFileTypeDoc,
		},
		{
			name:     "video requires mp4",
			fileName: "clip.mp4",
			semantic: imUploadedFileSemanticVideo,
			want:     larkim.CreateFileFileTypeMp4,
		},
		{
			name:     "video rejects non-mp4",
			fileName: "clip.mov",
			semantic: imUploadedFileSemanticVideo,
			wantErr:  "video messages require an .mp4 file",
		},
		{
			name:     "audio requires opus",
			fileName: "voice.opus",
			semantic: imUploadedFileSemanticAudio,
			want:     larkim.CreateFileFileTypeOpus,
		},
		{
			name:     "audio rejects non-opus",
			fileName: "voice.mp3",
			semantic: imUploadedFileSemanticAudio,
			wantErr:  "audio messages require an .opus file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := imFileTypeForSemantic(tt.fileName, tt.semantic)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("imFileTypeForSemantic(%q, %q) err = %v, want %q", tt.fileName, tt.semantic, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("imFileTypeForSemantic(%q, %q) unexpected err: %v", tt.fileName, tt.semantic, err)
			}
			if got != tt.want {
				t.Fatalf("imFileTypeForSemantic(%q, %q) = %q, want %q", tt.fileName, tt.semantic, got, tt.want)
			}
		})
	}
}
