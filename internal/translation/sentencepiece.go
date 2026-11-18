package translation

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Marian uses a shared source/target vocabulary and an unigram source tokenizer.
// The exported ONNX graphs accept token IDs, so the tokenizer stays in Go.
type sentencePiece struct {
	vocabulary []string
	pieceIDs   map[string]int64
	scores     map[string]float32
	maxBytes   int
	unknown    int64
	start      int64
	end        int64
}

func openSentencePiece(modelPath, vocabularyPath string) (sentencePiece, error) {
	var model sentencePiece
	modelData, err := os.ReadFile(modelPath)
	if err != nil {
		return model, err
	}
	model.scores, err = sentencePieceScores(modelData)
	if err != nil {
		return model, err
	}
	vocabulary, err := os.ReadFile(vocabularyPath)
	if err != nil {
		return model, err
	}
	if err := json.Unmarshal(vocabulary, &model.pieceIDs); err != nil {
		return model, err
	}
	var maxID int64
	for _, id := range model.pieceIDs {
		if id < 0 || id >= 100_000 {
			return model, errors.New("invalid translation vocabulary ID")
		}
		maxID = max(maxID, id)
	}
	model.vocabulary = make([]string, maxID+1)
	for piece, id := range model.pieceIDs {
		model.vocabulary[id] = piece
		if _, ok := model.scores[piece]; ok {
			model.maxBytes = max(model.maxBytes, len(piece))
		}
	}
	for _, special := range []struct {
		name string
		id   *int64
	}{{"<unk>", &model.unknown}, {"<pad>", &model.start}, {"</s>", &model.end}} {
		id, ok := model.pieceIDs[special.name]
		if !ok {
			return model, fmt.Errorf("translation vocabulary is missing %s", special.name)
		}
		*special.id = id
	}
	return model, nil
}

func (m sentencePiece) encode(text string) []int64 {
	prepared := "▁" + strings.Join(strings.Fields(norm.NFKC.String(text)), "▁")
	positions := make([]int, 0, utf8.RuneCountInString(prepared)+1)
	for i := range prepared {
		positions = append(positions, i)
	}
	positions = append(positions, len(prepared))
	type step struct {
		score float32
		from  int
		id    int64
	}
	path := make([]step, len(positions))
	for i := 1; i < len(path); i++ {
		path[i].score = float32(math.Inf(-1))
	}
	for i := 0; i < len(positions)-1; i++ {
		// Missing code points can consume one character without losing known text around them.
		if score := path[i].score - 100; score > path[i+1].score {
			path[i+1] = step{score, i, m.unknown}
		}
		for j := i + 1; j < len(positions) && positions[j]-positions[i] <= m.maxBytes; j++ {
			piece := prepared[positions[i]:positions[j]]
			pieceScore, ok := m.scores[piece]
			if id, found := m.pieceIDs[piece]; ok && found && path[i].score+pieceScore > path[j].score {
				path[j] = step{path[i].score + pieceScore, i, id}
			}
		}
	}
	ids := make([]int64, 0, len(positions))
	for i := len(path) - 1; i > 0; i = path[i].from {
		ids = append(ids, path[i].id)
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	return append(ids, m.end)
}

func (m sentencePiece) decode(ids []int64) string {
	var result strings.Builder
	for _, id := range ids {
		if id == m.start || id == m.end || id == m.unknown || id < 0 || id >= int64(len(m.vocabulary)) {
			continue
		}
		piece := m.vocabulary[id]
		if len(piece) == 6 && strings.HasPrefix(piece, "<0x") && piece[5] == '>' {
			if value, err := strconv.ParseUint(piece[3:5], 16, 8); err == nil {
				result.WriteByte(byte(value))
				continue
			}
		}
		result.WriteString(piece)
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(result.String(), "�"), "▁", " "))
}

var errSentencePiece = errors.New("invalid SentencePiece translation tokenizer")

// ModelProto.pieces is field one, repeated messages with a UTF-8 token, a
// fixed32 score and an enum type. Other metadata does not affect the Marian unigram path.
func sentencePieceScores(model []byte) (map[string]float32, error) {
	scores := make(map[string]float32)
	for len(model) > 0 {
		field, wire, value, rest, ok := protoField(model)
		if !ok {
			return nil, errSentencePiece
		}
		model = rest
		if field != 1 || wire != 2 {
			continue
		}
		var piece string
		var score float32
		kind := uint64(1) // normal piece (default)
		for len(value) > 0 {
			field, wire, data, rest, ok := protoField(value)
			if !ok {
				return nil, errSentencePiece
			}
			value = rest
			switch {
			case field == 1 && wire == 2:
				piece = string(data)
			case field == 2 && wire == 5:
				score = math.Float32frombits(binary.LittleEndian.Uint32(data))
			case field == 3 && wire == 0:
				kind, _ = binary.Uvarint(data)
			}
		}
		if piece != "" && (kind == 1 || kind == 4) && !math.IsNaN(float64(score)) {
			scores[piece] = score
		}
	}
	if len(scores) == 0 {
		return nil, errSentencePiece
	}
	return scores, nil
}

func protoField(data []byte) (field uint64, wire uint64, value, rest []byte, ok bool) {
	key, read := binary.Uvarint(data)
	if read <= 0 || key>>3 == 0 {
		return 0, 0, nil, nil, false
	}
	data = data[read:]
	var n int
	switch key & 7 {
	case 0:
		_, n = binary.Uvarint(data)
		if n <= 0 {
			return 0, 0, nil, nil, false
		}
	case 1:
		n = 8
	case 2:
		length, read := binary.Uvarint(data)
		if read <= 0 || length > uint64(len(data)-read) {
			return 0, 0, nil, nil, false
		}
		data, n = data[read:], int(length)
	case 5:
		n = 4
	default:
		return 0, 0, nil, nil, false
	}
	if n > len(data) {
		return 0, 0, nil, nil, false
	}
	return key >> 3, key & 7, data[:n], data[n:], true
}
