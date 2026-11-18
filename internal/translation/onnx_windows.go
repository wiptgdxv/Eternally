package translation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

type ortPointer = unsafe.Pointer

// OrtGetApiBase exposes an append-only C function table. The slot numbers in
// openONNX match ONNX Runtime 1.32.0's onnxruntime_c_api.h. All tensors are
// allocated by ORT, so no Go pointer is retained across native calls.
type onnxAPI struct {
	dll windows.Handle

	errorMessage       func(ortPointer) string
	createEnv          func(int32, string, *ortPointer) ortPointer
	disableTelemetry   func(ortPointer) ortPointer
	createSession      func(ortPointer, *uint16, ortPointer, *ortPointer) ortPointer
	run                func(ortPointer, ortPointer, **byte, *ortPointer, uintptr, **byte, uintptr, *ortPointer) ortPointer
	createSessionOpts  func(*ortPointer) ortPointer
	setGraphLevel      func(ortPointer, int32) ortPointer
	setIntraOpThreads  func(ortPointer, int32) ortPointer
	setInterOpThreads  func(ortPointer, int32) ortPointer
	createRunOptions   func(*ortPointer) ortPointer
	terminateRun       func(ortPointer) ortPointer
	createTensor       func(ortPointer, *int64, uintptr, int32, *ortPointer) ortPointer
	tensorData         func(ortPointer, *ortPointer) ortPointer
	tensorShape        func(ortPointer, *ortPointer) ortPointer
	dimensionCount     func(ortPointer, *uintptr) ortPointer
	dimensions         func(ortPointer, *int64, uintptr) ortPointer
	defaultAllocator   func(*ortPointer) ortPointer
	releaseStatus      func(ortPointer)
	releaseEnv         func(ortPointer)
	releaseSession     func(ortPointer)
	releaseValue       func(ortPointer)
	releaseRunOptions  func(ortPointer)
	releaseTensorShape func(ortPointer)
	releaseSessionOpts func(ortPointer)
	addSessionOption   func(ortPointer, string, string) ortPointer
}

func openONNX(path string) (*onnxAPI, error) {
	dll, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return nil, fmt.Errorf("加载本地翻译运行库失败。%w", err)
	}
	api := &onnxAPI{dll: dll}
	address, err := windows.GetProcAddress(dll, "OrtGetApiBase")
	if err != nil {
		api.close()
		return nil, err
	}
	var getBase func() *[1]uintptr
	purego.RegisterFunc(&getBase, address)
	base := getBase()
	if base == nil || base[0] == 0 {
		api.close()
		return nil, errors.New("本地翻译运行库接口不可用")
	}
	const apiVersion = 32
	var getAPI func(uint32) *[131]uintptr
	purego.RegisterFunc(&getAPI, base[0])
	table := getAPI(apiVersion)
	if table == nil {
		api.close()
		return nil, errors.New("本地翻译运行库版本不匹配")
	}
	for _, entry := range []struct {
		slot int
		bind any
	}{
		{2, &api.errorMessage}, {3, &api.createEnv}, {6, &api.disableTelemetry}, {7, &api.createSession},
		{9, &api.run}, {10, &api.createSessionOpts}, {23, &api.setGraphLevel}, {24, &api.setIntraOpThreads},
		{25, &api.setInterOpThreads}, {39, &api.createRunOptions}, {46, &api.terminateRun}, {48, &api.createTensor},
		{51, &api.tensorData}, {61, &api.dimensionCount}, {62, &api.dimensions}, {65, &api.tensorShape},
		{78, &api.defaultAllocator}, {92, &api.releaseEnv}, {93, &api.releaseStatus}, {95, &api.releaseSession},
		{96, &api.releaseValue}, {97, &api.releaseRunOptions}, {99, &api.releaseTensorShape},
		{100, &api.releaseSessionOpts}, {130, &api.addSessionOption},
	} {
		if table[entry.slot] == 0 {
			api.close()
			return nil, errors.New("本地翻译运行库接口不完整")
		}
		purego.RegisterFunc(entry.bind, table[entry.slot])
	}
	return api, nil
}

func (api *onnxAPI) close() { _ = windows.FreeLibrary(api.dll) }

func (api *onnxAPI) check(status ortPointer) error {
	if status == nil {
		return nil
	}
	defer api.releaseStatus(status)
	return errors.New(api.errorMessage(status))
}

type marianEngine struct {
	api       *onnxAPI
	env       ortPointer
	encoder   ortPointer
	decoder   ortPointer
	allocator ortPointer
	tokenizer sentencePiece
}

func openMarian(directory string) (_ *marianEngine, loadErr error) {
	model, err := openSentencePiece(filepath.Join(directory, sourceFile), filepath.Join(directory, vocabularyFile))
	if err != nil {
		return nil, err
	}
	api, err := openONNX(filepath.Join(directory, onnxRuntimeFile))
	if err != nil {
		return nil, err
	}
	engine := &marianEngine{api: api, tokenizer: model}
	defer func() {
		if loadErr != nil {
			engine.close()
		}
	}()
	const logErrors = 3
	if err := api.check(api.createEnv(logErrors, "ContextIME", &engine.env)); err != nil {
		return nil, err
	}
	if err := api.check(api.disableTelemetry(engine.env)); err != nil {
		return nil, err
	}
	if err := api.check(api.defaultAllocator(&engine.allocator)); err != nil {
		return nil, err
	}
	var options ortPointer
	if err := api.check(api.createSessionOpts(&options)); err != nil {
		return nil, err
	}
	defer api.releaseSessionOpts(options)
	const optimizeAll = 99
	for _, configure := range []func() ortPointer{
		func() ortPointer { return api.setIntraOpThreads(options, 2) },
		func() ortPointer { return api.setInterOpThreads(options, 1) },
		func() ortPointer { return api.setGraphLevel(options, optimizeAll) },
		func() ortPointer { return api.addSessionOption(options, "session.intra_op.allow_spinning", "0") },
	} {
		if err := api.check(configure()); err != nil {
			return nil, err
		}
	}
	for _, session := range []struct {
		file  string
		value *ortPointer
	}{{encoderFile, &engine.encoder}, {decoderFile, &engine.decoder}} {
		path, err := windows.UTF16PtrFromString(filepath.Join(directory, session.file))
		if err != nil {
			return nil, err
		}
		if err := api.check(api.createSession(engine.env, path, options, session.value)); err != nil {
			return nil, err
		}
	}
	return engine, nil
}

func (e *marianEngine) close() {
	if e.decoder != nil {
		e.api.releaseSession(e.decoder)
	}
	if e.encoder != nil {
		e.api.releaseSession(e.encoder)
	}
	if e.env != nil {
		e.api.releaseEnv(e.env)
	}
	e.api.close()
}

func (e *marianEngine) intTensor(values []int64) (ortPointer, error) {
	const int64DType = 7
	shape := [...]int64{1, int64(len(values))}
	var tensor, data ortPointer
	if err := e.api.check(e.api.createTensor(e.allocator, &shape[0], uintptr(len(shape)), int64DType, &tensor)); err != nil {
		return nil, err
	}
	if err := e.api.check(e.api.tensorData(tensor, &data)); err != nil {
		e.api.releaseValue(tensor)
		return nil, err
	}
	copy(unsafe.Slice((*int64)(data), len(values)), values)
	return tensor, nil
}

var marianEncoderInputs = onnxNames("input_ids", "attention_mask")
var marianEncoderOutput = onnxNames("last_hidden_state")
var marianDecoderInputs = onnxNames("input_ids", "encoder_attention_mask", "encoder_hidden_states")
var marianDecoderOutput = onnxNames("logits")

func onnxNames(names ...string) []*byte {
	result := make([]*byte, len(names))
	for i, name := range names {
		result[i], _ = windows.BytePtrFromString(name)
	}
	return result
}

func (e *marianEngine) run(ctx context.Context, session, options ortPointer, inputNames, outputName []*byte, inputs ...ortPointer) (ortPointer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out ortPointer
	err := e.api.check(e.api.run(session, options, &inputNames[0], &inputs[0], uintptr(len(inputs)), &outputName[0], 1, &out))
	if canceled := ctx.Err(); canceled != nil {
		err = canceled
	}
	if err != nil {
		if out != nil {
			e.api.releaseValue(out)
		}
		return nil, err
	}
	if out == nil {
		return nil, errors.New("本地翻译运行库未返回输出")
	}
	return out, nil
}

func (e *marianEngine) nextToken(output ortPointer, tokenCount int) (int64, error) {
	var shape, data ortPointer
	if err := e.api.check(e.api.tensorShape(output, &shape)); err != nil {
		return 0, err
	}
	defer e.api.releaseTensorShape(shape)
	var rank uintptr
	if err := e.api.check(e.api.dimensionCount(shape, &rank)); err != nil {
		return 0, err
	}
	if rank != 3 {
		return 0, errors.New("本地翻译输出格式无效")
	}
	var dims [3]int64
	if err := e.api.check(e.api.dimensions(shape, &dims[0], rank)); err != nil {
		return 0, err
	}
	if dims[0] != 1 || dims[1] != int64(tokenCount) || dims[2] != int64(len(e.tokenizer.vocabulary)) {
		return 0, errors.New("本地翻译输出尺寸无效")
	}
	if err := e.api.check(e.api.tensorData(output, &data)); err != nil {
		return 0, err
	}
	count := int(dims[2])
	logits := unsafe.Slice((*float32)(data), tokenCount*count)[(tokenCount-1)*count:]
	best := int64(-1)
	for id, value := range logits {
		if int64(id) != e.tokenizer.start && !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) && (best < 0 || value > logits[best]) {
			best = int64(id)
		}
	}
	if best < 0 {
		return 0, errors.New("本地翻译输出无有效词片")
	}
	return best, nil
}

func (e *marianEngine) translate(ctx context.Context, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("请输入要离线翻译的文字")
	}
	ids := e.tokenizer.encode(text)
	if len(ids) > 512 {
		return "", errors.New("文字过长，请分段离线翻译")
	}
	var options ortPointer
	if err := e.api.check(e.api.createRunOptions(&options)); err != nil {
		return "", err
	}
	defer e.api.releaseRunOptions(options)
	terminated := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = e.api.check(e.api.terminateRun(options))
		close(terminated)
	})
	defer func() {
		if !stop() {
			<-terminated
		}
	}()
	input, err := e.intTensor(ids)
	if err != nil {
		return "", err
	}
	defer e.api.releaseValue(input)
	mask := make([]int64, len(ids))
	for i := range mask {
		mask[i] = 1
	}
	attention, err := e.intTensor(mask)
	if err != nil {
		return "", err
	}
	defer e.api.releaseValue(attention)
	hidden, err := e.run(ctx, e.encoder, options, marianEncoderInputs, marianEncoderOutput, input, attention)
	if err != nil {
		return "", err
	}
	defer e.api.releaseValue(hidden)
	tokens := []int64{e.tokenizer.start}
	limit := min(160, 4*len(ids)+16)
	for range limit {
		tensor, err := e.intTensor(tokens)
		if err != nil {
			return "", err
		}
		output, err := e.run(ctx, e.decoder, options, marianDecoderInputs, marianDecoderOutput, tensor, attention, hidden)
		e.api.releaseValue(tensor)
		if err != nil {
			return "", err
		}
		next, err := e.nextToken(output, len(tokens))
		e.api.releaseValue(output)
		if err != nil {
			return "", err
		}
		if next == e.tokenizer.end {
			result := e.tokenizer.decode(tokens[1:])
			if result == "" {
				return "", errors.New("本地模型未返回译文")
			}
			return result, nil
		}
		tokens = append(tokens, next)
	}
	return "", errors.New("本地模型未能完整生成译文")
}
