#define COBJMACROS
#include <windows.h>
#include <msctf.h>
#include <ctfutb.h>
#include <oleauto.h>
#include <olectl.h>
#include <stddef.h>
#include <stdint.h>
#include <wchar.h>

// Older MinGW headers omit this interface from msctf.h.
#ifndef __ITfTextLayoutSink_INTERFACE_DEFINED__
typedef struct ITfTextLayoutSink ITfTextLayoutSink;
typedef enum { TF_LC_CREATE, TF_LC_CHANGE, TF_LC_DESTROY } TfLayoutCode;
typedef struct {
    HRESULT (STDMETHODCALLTYPE *QueryInterface)(ITfTextLayoutSink *,REFIID,void **);
    ULONG (STDMETHODCALLTYPE *AddRef)(ITfTextLayoutSink *);
    ULONG (STDMETHODCALLTYPE *Release)(ITfTextLayoutSink *);
    HRESULT (STDMETHODCALLTYPE *OnLayoutChange)(ITfTextLayoutSink *,ITfContext *,TfLayoutCode,ITfContextView *);
} ITfTextLayoutSinkVtbl;
struct ITfTextLayoutSink { ITfTextLayoutSinkVtbl *lpVtbl; };
#endif
static const IID text_layout_sink_id={0x2af2d06a,0xdd5b,0x4927,{0xa0,0xb4,0x54,0xf1,0x9c,0x91,0xfa,0xde}};

static const GUID service_id = {0xa65e6a23,0x596a,0x4535,{0xa2,0x4d,0x27,0x62,0xb2,0x67,0x97,0x74}};
static const GUID profile_id = {0x8c597e63,0xc615,0x4b1c,{0xac,0x11,0x31,0x8f,0x3a,0xab,0xd0,0x29}};
// Language bar interfaces and IDs missing from the MinGW headers.
typedef struct ModeButton ModeButton;
typedef struct {
    HRESULT (STDMETHODCALLTYPE *QueryInterface)(ModeButton *,REFIID,void **);
    ULONG (STDMETHODCALLTYPE *AddRef)(ModeButton *);
    ULONG (STDMETHODCALLTYPE *Release)(ModeButton *);
    HRESULT (STDMETHODCALLTYPE *GetInfo)(ModeButton *,TF_LANGBARITEMINFO *);
    HRESULT (STDMETHODCALLTYPE *GetStatus)(ModeButton *,DWORD *);
    HRESULT (STDMETHODCALLTYPE *Show)(ModeButton *,BOOL);
    HRESULT (STDMETHODCALLTYPE *GetTooltipString)(ModeButton *,BSTR *);
    HRESULT (STDMETHODCALLTYPE *OnClick)(ModeButton *,int,POINT,const RECT *);
    HRESULT (STDMETHODCALLTYPE *InitMenu)(ModeButton *,void *);
    HRESULT (STDMETHODCALLTYPE *OnMenuSelect)(ModeButton *,UINT);
    HRESULT (STDMETHODCALLTYPE *GetIcon)(ModeButton *,HICON *);
    HRESULT (STDMETHODCALLTYPE *GetText)(ModeButton *,BSTR *);
} ModeButtonVtbl;
struct ModeButton { const ModeButtonVtbl *lpVtbl; };
static const IID mode_button_id={0x28c7f1d0,0xde25,0x11d2,{0xaf,0xdd,0x00,0x10,0x5a,0x27,0x99,0xb5}};
static const GUID input_mode_item={0x2c77a81e,0x41cc,0x4178,{0xa3,0xa7,0x5f,0x8a,0x98,0x75,0x68,0xe6}};
static const GUID tray_support={0x25504fb4,0x7bab,0x4bc1,{0x9c,0x69,0xcf,0x81,0x89,0x0f,0x0e,0xf5}};
// The first icon group in ContextIME.rc is registered as profile icon index zero.
static HINSTANCE module;
static LONG objects;
static const WCHAR clsid_key[] = L"Software\\Classes\\CLSID\\{A65E6A23-596A-4535-A24D-2762B2679774}";
static const WCHAR server_key[] = L"Software\\Classes\\CLSID\\{A65E6A23-596A-4535-A24D-2762B2679774}\\InprocServer32";

#define SYMBOL_KEY_BASE 0x10000u
#define EDIT_KEY_BASE 0x20000u

typedef struct {
    ITfTextInputProcessor tip;
    ITfKeyEventSink keys;
    ITfThreadMgrEventSink focus;
    ITfTextLayoutSink layout;
    ModeButton mode_button;
    ITfSource mode_source;
    LONG refs;
    TfClientId client;
    ITfThreadMgr *manager;
    ITfKeystrokeMgr *keymgr;
    ITfSource *source;
    DWORD focus_cookie;
    ITfSource *layout_source;
    DWORD layout_cookie;
    BOOL caret_pending;
    BOOL caret_reported;
    RECT caret_bounds;
    HWND caret_control;
    HANDLE pipe;
    uint32_t seq;
    BOOL enter_down;
    BOOL english;
    BOOL caps_lock;
    BOOL buffered_source;
    BOOL translation_source;
    WCHAR target_badge;
    WCHAR target_tip[64];
    ITfLangBarItemMgr *bar;
    ITfLangBarItemSink *bar_sink;
    BYTE shortcut_down[256];
    BOOL shift_preserved;
    BOOL comma_preserved;
    BOOL shift_alone;
    BOOL shift_chord;
    BYTE shift_down;
    HHOOK shift_hook;
    uint32_t focus_epoch;
} Service;

#define OWNER(p,member) ((Service *)((char *)(p)-offsetof(Service,member)))
static ULONG retain(Service *s) { return InterlockedIncrement(&s->refs); }
static void disconnect(Service *s) {
    if (s->pipe != INVALID_HANDLE_VALUE) CloseHandle(s->pipe);
    s->pipe = INVALID_HANDLE_VALUE;
    s->enter_down = FALSE;
    s->focus_epoch++;
    s->caret_pending=FALSE;
    s->caret_reported=FALSE;
    if(s->layout_source) {
        ITfSource *source=s->layout_source;
        s->layout_source=NULL;
        ITfSource_UnadviseSink(source,s->layout_cookie);
        ITfSource_Release(source);
    }
}
static ULONG drop(Service *s) {
    LONG n = InterlockedDecrement(&s->refs);
    if (!n) { disconnect(s); HeapFree(GetProcessHeap(),0,s); InterlockedDecrement(&objects); }
    return n;
}
static HRESULT query(Service *s, REFIID id, void **out) {
    if (!out) return E_POINTER;
    *out = NULL;
    if (IsEqualGUID(id,&IID_IUnknown) || IsEqualGUID(id,&IID_ITfTextInputProcessor)) *out = &s->tip;
    else if (IsEqualGUID(id,&IID_ITfKeyEventSink)) *out = &s->keys;
    else if (IsEqualGUID(id,&IID_ITfThreadMgrEventSink)) *out = &s->focus;
    else if (IsEqualGUID(id,&text_layout_sink_id)) *out = &s->layout;
    if (!*out) return E_NOINTERFACE;
    retain(s);
    return S_OK;
}
#define COM_FACE(prefix, type, member) \
 static HRESULT STDMETHODCALLTYPE prefix##_query(type *p,REFIID id,void **out){return query(OWNER(p,member),id,out);} \
 static ULONG STDMETHODCALLTYPE prefix##_add(type *p){return retain(OWNER(p,member));} \
 static ULONG STDMETHODCALLTYPE prefix##_drop(type *p){return drop(OWNER(p,member));}
COM_FACE(tip, ITfTextInputProcessor, tip)
COM_FACE(keys, ITfKeyEventSink, keys)
COM_FACE(focus, ITfThreadMgrEventSink, focus)
COM_FACE(layout, ITfTextLayoutSink, layout)

static HRESULT STDMETHODCALLTYPE mode_button_query(ModeButton *p,REFIID id,void **out) {
    if (!out) return E_POINTER;
    *out=NULL;
    Service *s=OWNER(p,mode_button);
    if (IsEqualGUID(id,&IID_IUnknown) || IsEqualGUID(id,&IID_ITfLangBarItem) || IsEqualGUID(id,&mode_button_id)) *out=p;
    else if (IsEqualGUID(id,&IID_ITfSource)) *out=&s->mode_source;
    if (!*out) return E_NOINTERFACE;
    retain(s);
    return S_OK;
}
static ULONG STDMETHODCALLTYPE mode_button_add(ModeButton *p){return retain(OWNER(p,mode_button));}
static ULONG STDMETHODCALLTYPE mode_button_drop(ModeButton *p){return drop(OWNER(p,mode_button));}
static HRESULT STDMETHODCALLTYPE mode_source_query(ITfSource *p,REFIID id,void **out){return mode_button_query(&OWNER(p,mode_source)->mode_button,id,out);}
static ULONG STDMETHODCALLTYPE mode_source_add(ITfSource *p){return retain(OWNER(p,mode_source));}
static ULONG STDMETHODCALLTYPE mode_source_drop(ITfSource *p){return drop(OWNER(p,mode_source));}
static HRESULT STDMETHODCALLTYPE mode_advise(ITfSource *p,REFIID id,IUnknown *sink,DWORD *cookie) {
    if (!sink || !cookie) return E_INVALIDARG;
    if (!IsEqualGUID(id,&IID_ITfLangBarItemSink)) return CONNECT_E_CANNOTCONNECT;
    Service *s=OWNER(p,mode_source);
    if (s->bar_sink) return CONNECT_E_ADVISELIMIT;
    HRESULT hr=IUnknown_QueryInterface(sink,id,(void**)&s->bar_sink);
    if (SUCCEEDED(hr)) *cookie=1;
    return hr;
}
static void release_bar_sink(Service *s) {
    ITfLangBarItemSink *sink=s->bar_sink;
    s->bar_sink=NULL;
    if (sink) ITfLangBarItemSink_Release(sink);
}
static HRESULT STDMETHODCALLTYPE mode_unadvise(ITfSource *p,DWORD cookie) {
    Service *s=OWNER(p,mode_source);
    if (cookie!=1 || !s->bar_sink) return CONNECT_E_NOCONNECTION;
    release_bar_sink(s);
    return S_OK;
}
static ITfSourceVtbl mode_source_vtbl={mode_source_query,mode_source_add,mode_source_drop,mode_advise,mode_unadvise};

// Fixed-size local protocol. Each pipe is one TSF focus session.
typedef struct { uint32_t method, key, repeat, seq; } Request;
typedef struct { uint32_t handled, length; WCHAR text[512]; } Reply;

static BOOL transfer(HANDLE pipe, BOOL write, void *buffer, DWORD size, HANDLE event) {
    DWORD offset = 0;
    ULONGLONG end = GetTickCount64()+55;
    while (offset < size) {
        OVERLAPPED op = {0};
        op.hEvent=event;
        ResetEvent(event);
        DWORD count=0;
        BOOL ok=write ? WriteFile(pipe,(char*)buffer+offset,size-offset,&count,&op)
                      : ReadFile(pipe,(char*)buffer+offset,size-offset,&count,&op);
        if (!ok && GetLastError()==ERROR_IO_PENDING) {
            ULONGLONG now=GetTickCount64();
            if (WaitForSingleObject(event,now<end?(DWORD)(end-now):0)!=WAIT_OBJECT_0) {
                CancelIoEx(pipe,&op);
                GetOverlappedResult(pipe,&op,&count,TRUE);
                return FALSE;
            }
            ok=GetOverlappedResult(pipe,&op,&count,FALSE);
        }
        if (!ok || !count) return FALSE;
        offset+=count;
    }
    return TRUE;
}
static BOOL exchange_request(Service *s, Request req, Reply *reply) {
    if (s->pipe==INVALID_HANDLE_VALUE) {
        WCHAR name[128];
        DWORD session=0;
        if (!ProcessIdToSessionId(GetCurrentProcessId(),&session)) return FALSE;
        wsprintfW(name,L"\\\\.\\pipe\\ContextIME-TSF-v1-%lu",session);
        s->pipe=CreateFileW(name,GENERIC_READ|GENERIC_WRITE,0,NULL,OPEN_EXISTING,FILE_FLAG_OVERLAPPED,NULL);
        if (s->pipe==INVALID_HANDLE_VALUE) return FALSE;
    }
    HANDLE event=CreateEventW(NULL,TRUE,FALSE,NULL);
    if (!event) { disconnect(s); return FALSE; }
    ZeroMemory(reply,sizeof(*reply));
    BOOL ok=transfer(s->pipe,TRUE,&req,sizeof(req),event)
         && transfer(s->pipe,FALSE,reply,8,event)
         && reply->length<=512;
    if (ok && reply->length) ok=transfer(s->pipe,FALSE,reply->text,reply->length*sizeof(WCHAR),event);
    CloseHandle(event);
    if (!ok) disconnect(s);
    return ok;
}
static BOOL exchange(Service *s, uint32_t method, WPARAM key, LPARAM flags, Reply *reply) {
    Request req={method,(uint32_t)key,(flags & (1L<<30))!=0,++s->seq};
    return exchange_request(s,req,reply);
}

static void notify_input_mode(Service *s) {
    if (s->bar_sink) ITfLangBarItemSink_OnUpdate(s->bar_sink,0x00000007); // icon, text, tooltip
}
static void refresh_caps_lock(Service *s) {
    BOOL caps_lock=(GetKeyState(VK_CAPITAL)&1)!=0;
    if (s->caps_lock==caps_lock) return;
    s->caps_lock=caps_lock;
    notify_input_mode(s);
}
static void refresh_input_mode(Service *s) {
    refresh_caps_lock(s);
    Reply reply;
    if (!exchange(s,9,0,0,&reply)) return;
    BOOL english=reply.handled!=0;
    BOOL buffered_source=FALSE;
    BOOL translation_source=FALSE;
    WCHAR badge=0;
    WCHAR tip[64]={0};
    // Older backends return only the direct-English flag.
    if (reply.length>2 && reply.text[0] && !reply.text[1]) {
        badge=reply.text[0];
        uint32_t end=2;
        while (end<reply.length && reply.text[end]) end++;
        uint32_t count=end-2;
        if (count>=ARRAYSIZE(tip)) count=ARRAYSIZE(tip)-1;
        CopyMemory(tip,reply.text+2,count*sizeof(WCHAR));
        // The optional third field selects a raw-text draft instead of pinyin.
        buffered_source=end+1<reply.length && reply.text[end+1]==L'1';
        translation_source=end+1<reply.length && (reply.text[end+1]==L'1' || reply.text[end+1]==L'2');
    }
    if (s->english==english && s->buffered_source==buffered_source && s->translation_source==translation_source &&
        s->target_badge==badge && !wcscmp(s->target_tip,tip)) return;
    s->english=english;
    s->buffered_source=buffered_source;
    s->translation_source=translation_source;
    s->target_badge=badge;
    lstrcpynW(s->target_tip,tip,ARRAYSIZE(s->target_tip));
    notify_input_mode(s);
}
static void toggle_input_mode(Service *s) {
    Reply reply;
    if (exchange(s,10,0,0,&reply) && reply.handled) refresh_input_mode(s);
}
static _Thread_local Service *shift_service;

static const GUID toggle_comma_id={0xdd65f3a1,0x24d5,0x4374,{0xac,0x38,0x23,0xe3,0x36,0x5c,0x12,0xd9}};
static const TF_PRESERVEDKEY toggle_comma_key={VK_OEM_COMMA,TF_MOD_CONTROL};
static void trace_result(const char *label,HRESULT a,HRESULT b);

static BOOL is_shift(WPARAM key) {
    return key==VK_SHIFT || key==VK_LSHIFT || key==VK_RSHIFT;
}

static BOOL shift_keyboard_down(void) {
    return (GetKeyState(VK_SHIFT)&0x8000) || (GetKeyState(VK_LSHIFT)&0x8000) ||
        (GetKeyState(VK_RSHIFT)&0x8000);
}

static void trace_shift_state(const char *label,Service *s) {
    if(GetEnvironmentVariableW(L"CONTEXTIME_TSF_TRACE",NULL,0)==0) return;
    // First value: left/right held bits, alone 0x100, chord 0x200.
    // Second value: queued Shift/left/right keyboard-state bits 1/2/4.
    uint32_t state=s->shift_down | (s->shift_alone?0x100:0) | (s->shift_chord?0x200:0);
    uint32_t keyboard=((GetKeyState(VK_SHIFT)&0x8000)?1:0) |
        ((GetKeyState(VK_LSHIFT)&0x8000)?2:0) | ((GetKeyState(VK_RSHIFT)&0x8000)?4:0);
    trace_result(label,(HRESULT)state,(HRESULT)keyboard);
}

static void reset_shift(Service *s) {
    s->shift_alone=FALSE;
    s->shift_chord=FALSE;
    s->shift_down=0;
}

static BYTE shift_side(WPARAM key,LPARAM flags) {
    // WM_KEY* normally uses VK_SHIFT; TSF may report a left/right virtual key.
    return key==VK_RSHIFT || (key==VK_SHIFT && (((UINT)flags>>16)&0xff)==0x36) ? 2 : 1;
}

static void cancel_shift_tap(Service *s) {
    if (s->shift_down || shift_keyboard_down()) {
        s->shift_alone=FALSE;
        s->shift_chord=TRUE;
        trace_shift_state("shift-chord-state",s);
    }
}

static void track_shift_down(Service *s, WPARAM key, LPARAM flags) {
    if (!is_shift(key)) {
        if (s->shift_down || shift_keyboard_down()) trace_result("shift-cancel-down",(HRESULT)key,(HRESULT)flags);
        cancel_shift_tap(s);
        return;
    }
    trace_shift_state("shift-down-before",s);
    BYTE side=shift_side(key,flags);
    // A physical press can be reported by both the message hook and TSF. Once
    // a chord cancels it, another notification must not arm that press again.
    if (s->shift_down&side) return;
    BOOL first=s->shift_down==0;
    s->shift_down|=side;
    if (!first || (flags&(1L<<30))) s->shift_chord=TRUE;
    s->shift_alone=first && !s->shift_chord &&
        !(GetKeyState(VK_CONTROL)&0x8000) && !(GetKeyState(VK_MENU)&0x8000) &&
        !(GetKeyState(VK_LWIN)&0x8000) && !(GetKeyState(VK_RWIN)&0x8000);
    trace_shift_state("shift-down-after",s);
}

static BOOL complete_shift(Service *s) {
    trace_shift_state("shift-complete-state",s);
    BOOL alone=s->shift_alone && !s->shift_chord;
    reset_shift(s);
    if (!alone) return FALSE;
    toggle_input_mode(s);
    return TRUE;
}

static BOOL finish_shift(Service *s) {
    // A preserved key-up can consume WM_KEYUP before queue keyboard state is
    // updated. The observed press/chord latch is authoritative for this event.
    trace_shift_state("shift-preserved-state",s);
    return complete_shift(s);
}

static BOOL track_shift_up(Service *s,WPARAM key,LPARAM flags) {
    if (!is_shift(key)) {
        // TSF can report synthetic or unrelated releases before the preserved
        // Shift release. Only an observed press/character proves a chord.
        if (s->shift_down) trace_result("shift-ignore-up",(HRESULT)key,(HRESULT)flags);
        return FALSE;
    }
    trace_shift_state("shift-up-before",s);
    s->shift_down&=(BYTE)~shift_side(key,flags);
    if (s->shift_down) return FALSE;
    return complete_shift(s);
}

static BOOL shortcut_key(WPARAM key) {
    return (key>=VK_F6 && key<=VK_F12) || key==VK_OEM_COMMA || key==VK_OEM_PERIOD;
}

static BOOL ctrl_only(void) {
    return (GetKeyState(VK_CONTROL)&0x8000) && !(GetKeyState(VK_SHIFT)&0x8000)
        && !(GetKeyState(VK_MENU)&0x8000) && !(GetKeyState(VK_LWIN)&0x8000)
        && !(GetKeyState(VK_RWIN)&0x8000);
}

static BOOL shortcut_held(Service *s,WPARAM key) {
    return shortcut_key(key) && s->shortcut_down[key];
}

static BOOL release_shortcut(Service *s,WPARAM key) {
    if (!shortcut_held(s,key)) return FALSE;
    s->shortcut_down[key]=FALSE;
    return TRUE;
}

static BOOL press_shortcut(Service *s,WPARAM key,LPARAM flags) {
    if (shortcut_held(s,key)) return TRUE;
    if (flags&(1L<<30)) return FALSE;
    Reply reply;
    if (!exchange(s,2,EDIT_KEY_BASE+key,flags,&reply) || !reply.handled) return FALSE;
    s->shortcut_down[key]=TRUE;
    refresh_input_mode(s);
    return TRUE;
}

static BOOL compartment_enabled(ITfCompartmentMgr *manager,REFGUID id) {
    ITfCompartment *compartment=NULL;
    if(ITfCompartmentMgr_GetCompartment(manager,id,&compartment)!=S_OK || !compartment) return FALSE;
    VARIANT value={0};
    HRESULT hr=ITfCompartment_GetValue(compartment,&value);
    BOOL enabled=hr==S_OK && V_VT(&value)==VT_I4 && V_I4(&value)!=0;
    VariantClear(&value);
    ITfCompartment_Release(compartment);
    return enabled;
}

static BOOL editable_context_focused(Service *s) {
    CLSID foreground;
    if (!s->keymgr || !s->manager) return FALSE;
    HRESULT hr=ITfKeystrokeMgr_GetForeground(s->keymgr,&foreground);
    BOOL active=hr==S_OK && IsEqualGUID(&foreground,&service_id);
    trace_result("shortcut-profile",hr,active);
    if(!active) return FALSE;

    ITfDocumentMgr *document=NULL;
    hr=ITfThreadMgr_GetFocus(s->manager,&document);
    if(hr!=S_OK || !document) {
        trace_result("shortcut-document",hr,0);
        return FALSE;
    }
    ITfContext *context=NULL;
    hr=ITfDocumentMgr_GetTop(document,&context);
    ITfDocumentMgr_Release(document);
    if(hr!=S_OK || !context) {
        trace_result("shortcut-context",hr,0);
        return FALSE;
    }
    TF_STATUS status={0};
    hr=ITfContext_GetStatus(context,&status);
    BOOL editable=hr==S_OK && !(status.dwDynamicFlags&TF_SD_READONLY);
    if(editable) {
        ITfCompartmentMgr *compartments=NULL;
        if(ITfContext_QueryInterface(context,&IID_ITfCompartmentMgr,(void**)&compartments)==S_OK && compartments) {
            editable=!compartment_enabled(compartments,&GUID_COMPARTMENT_KEYBOARD_DISABLED)
                && !compartment_enabled(compartments,&GUID_COMPARTMENT_EMPTYCONTEXT);
            ITfCompartmentMgr_Release(compartments);
        }
    }
    ITfContext_Release(context);
    trace_result("shortcut-editable",hr,editable);
    return editable;
}

// Chromium can pass these messages through without calling the TSF key sink.
// Observe only the active text service's own Windows message queue.
static LRESULT CALLBACK shift_message(int code, WPARAM action, LPARAM data) {
    Service *s=shift_service;
    if (code==HC_ACTION && action==PM_REMOVE && s) {
        MSG *msg=(MSG*)data;
        DWORD window_thread=GetWindowThreadProcessId(GetForegroundWindow(),NULL);
        DWORD input_thread=GetCurrentThreadId();
        BOOL key_down=msg->message==WM_KEYDOWN || msg->message==WM_SYSKEYDOWN;
        BOOL key_up=msg->message==WM_KEYUP || msg->message==WM_SYSKEYUP;
        if((key_down || key_up) && (is_shift(msg->wParam) || shortcut_key(msg->wParam))) {
            trace_result("shortcut-hook",(HRESULT)msg->message,(HRESULT)msg->wParam);
            trace_result("shortcut-thread",window_thread,input_thread);
            trace_result("shortcut-modifiers",ctrl_only(),s->shift_alone);
        }
        HWND focused=GetFocus();
        BOOL foreground_input=window_thread==input_thread || (focused && GetAncestor(focused,GA_ROOT)==GetForegroundWindow());
        if (foreground_input) {
            if (key_down) {
                track_shift_down(s,msg->wParam,msg->lParam);
                if (shortcut_held(s,msg->wParam) ||
                    (shortcut_key(msg->wParam) && !(msg->lParam&(1L<<30)) && ctrl_only()
                     && editable_context_focused(s) && press_shortcut(s,msg->wParam,msg->lParam))) {
                    msg->message=WM_NULL;
                }
            } else if (key_up) {
                track_shift_up(s,msg->wParam,msg->lParam);
                if (release_shortcut(s,msg->wParam)) msg->message=WM_NULL;
            } else if (((msg->message==WM_CHAR || msg->message==WM_SYSCHAR ||
                        msg->message==WM_DEADCHAR || msg->message==WM_SYSDEADCHAR ||
                        (msg->message==WM_UNICHAR && msg->wParam!=UNICODE_NOCHAR)) &&
                        msg->wParam>=0x20 && msg->wParam!=0x7f) ||
                       (msg->message>=WM_LBUTTONDOWN && msg->message<=WM_MBUTTONDBLCLK) ||
                       (msg->message>=WM_XBUTTONDOWN && msg->message<=WM_XBUTTONDBLCLK) ||
                       (msg->message>=WM_NCLBUTTONDOWN && msg->message<=WM_NCMBUTTONDBLCLK) ||
                       (msg->message>=WM_NCXBUTTONDOWN && msg->message<=WM_NCXBUTTONDBLCLK)) {
                if (s->shift_down || shift_keyboard_down()) trace_result("shift-cancel-message",(HRESULT)msg->message,(HRESULT)msg->wParam);
                cancel_shift_tap(s);
            }
        } else {
            reset_shift(s);
            ZeroMemory(s->shortcut_down,sizeof(s->shortcut_down));
        }
    }
    return CallNextHookEx(NULL,code,action,data);
}
static HRESULT STDMETHODCALLTYPE mode_info(ModeButton *p,TF_LANGBARITEMINFO *info) {
    if (!info) return E_POINTER;
    *info=(TF_LANGBARITEMINFO){service_id,input_mode_item,0x00010002,0,L"Eternally"}; // button shown in tray
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE mode_status(ModeButton *p,DWORD *status) {
    if (!status) return E_POINTER;
    *status=0;
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE mode_show(ModeButton *p,BOOL show){return S_OK;}
static const WCHAR *ordinary_mode_label(Service *s) {
    return s->caps_lock?L"\x5927\x5199":s->english?L"\x82f1":L"\x4e2d";
}
static HRESULT STDMETHODCALLTYPE mode_tooltip(ModeButton *p,BSTR *text) {
    if (!text) return E_POINTER;
    Service *s=OWNER(p,mode_button);
    *text=SysAllocString(!s->caps_lock && s->target_tip[0]?s->target_tip:ordinary_mode_label(s));
    return *text?S_OK:E_OUTOFMEMORY;
}
static HRESULT STDMETHODCALLTYPE mode_text(ModeButton *p,BSTR *text) {
    if (!text) return E_POINTER;
    Service *s=OWNER(p,mode_button);
    *text=!s->caps_lock && s->target_badge
        ?SysAllocStringLen(&s->target_badge,1)
        :SysAllocString(ordinary_mode_label(s));
    return *text?S_OK:E_OUTOFMEMORY;
}
static HRESULT STDMETHODCALLTYPE mode_click(ModeButton *p,int click,POINT point,const RECT *area) {
    Service *s=OWNER(p,mode_button);
    if (click==2) toggle_input_mode(s); // TF_LBI_CLK_LEFT
    if (click==1) { // TF_LBI_CLK_RIGHT
        Reply reply;
        exchange(s,11,0,0,&reply);
    }
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE mode_menu(ModeButton *p,void *menu){return E_NOTIMPL;}
static HRESULT STDMETHODCALLTYPE mode_select(ModeButton *p,UINT item){return E_NOTIMPL;}
static HICON make_target_icon(WCHAR badge) {
    enum { size=16, count=size*size };
    BITMAPINFO info={0};
    info.bmiHeader=(BITMAPINFOHEADER){sizeof(BITMAPINFOHEADER),size,-size,1,32,BI_RGB};
    DWORD *pixels=NULL;
    HDC dc=CreateCompatibleDC(NULL);
    HBITMAP bitmap=CreateDIBSection(dc,&info,DIB_RGB_COLORS,(void**)&pixels,NULL,0);
    WORD opaque_mask[size]={0};
    HBITMAP mask=CreateBitmap(size,size,1,1,opaque_mask);
    HFONT font=CreateFontW(-14,0,0,0,FW_SEMIBOLD,FALSE,FALSE,FALSE,DEFAULT_CHARSET,
                           OUT_DEFAULT_PRECIS,CLIP_DEFAULT_PRECIS,ANTIALIASED_QUALITY,
                           DEFAULT_PITCH,L"Microsoft YaHei UI");
    HICON icon=NULL;
    if (dc && bitmap && mask && font) {
        HGDIOBJ old_bitmap=SelectObject(dc,bitmap);
        HGDIOBJ old_font=SelectObject(dc,font);
        if (old_bitmap && old_font) {
            RECT bounds={0,0,size,size};
            ZeroMemory(pixels,count*sizeof(*pixels));
            SetBkMode(dc,TRANSPARENT);
            SetTextColor(dc,RGB(255,255,255));
            DrawTextW(dc,&badge,1,&bounds,DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX);
            GdiFlush();
            BYTE coverage[count];
            for (int i=0;i<count;i++) coverage[i]=(BYTE)pixels[i];
            // A white outline keeps the dark letter readable on either taskbar theme.
            for (int y=0;y<size;y++) for (int x=0;x<size;x++) {
                int i=y*size+x;
                unsigned ink=coverage[i], halo=0;
                if (x>0) halo=max(halo,coverage[i-1]);
                if (x<size-1) halo=max(halo,coverage[i+1]);
                if (y>0) halo=max(halo,coverage[i-size]);
                if (y<size-1) halo=max(halo,coverage[i+size]);
                unsigned outline=halo*(255-ink)/255;
                unsigned alpha=ink+outline;
                pixels[i]=(alpha<<24)|((outline+ink*34/255)<<16)
                    |((outline+ink*39/255)<<8)|(outline+ink*47/255);
            }
        }
        if (old_font) SelectObject(dc,old_font);
        if (old_bitmap) SelectObject(dc,old_bitmap);
        if (old_bitmap && old_font) {
            ICONINFO data={TRUE,0,0,mask,bitmap};
            icon=CreateIconIndirect(&data);
        }
    }
    if (font) DeleteObject(font);
    if (mask) DeleteObject(mask);
    if (bitmap) DeleteObject(bitmap);
    if (dc) DeleteDC(dc);
    return icon;
}
static HRESULT STDMETHODCALLTYPE mode_icon(ModeButton *p,HICON *icon) {
    if (!icon) return E_POINTER;
    Service *s=OWNER(p,mode_button);
    if (!s->caps_lock && s->target_badge && s->target_badge!=L'\x82f1') {
        *icon=make_target_icon(s->target_badge);
        if (*icon) return S_OK;
    }
    const WORD resource=s->caps_lock?104:s->english || s->target_badge==L'\x82f1'?103:102;
    *icon=(HICON)LoadImageW(module,MAKEINTRESOURCEW(resource),IMAGE_ICON,16,16,0);
    return *icon?S_OK:E_FAIL;
}
static const ModeButtonVtbl mode_button_vtbl={
    mode_button_query,mode_button_add,mode_button_drop,mode_info,mode_status,mode_show,mode_tooltip,
    mode_click,mode_menu,mode_select,mode_icon,mode_text
};

typedef struct {
    ITfEditSession iface;
    LONG refs;
    ITfContext *context;
    Reply reply;
    Service *service;
    uint32_t epoch;
    HWND window;
    HWND control;
    BOOL notified;
    BOOL caret;
} Edit;
static HRESULT STDMETHODCALLTYPE edit_query(ITfEditSession *p,REFIID id,void **out) {
    if (!out) return E_POINTER;
    *out=NULL;
    if (!IsEqualGUID(id,&IID_IUnknown) && !IsEqualGUID(id,&IID_ITfEditSession)) return E_NOINTERFACE;
    *out=p;
    ITfEditSession_AddRef(p);
    return S_OK;
}
static ULONG STDMETHODCALLTYPE edit_add(ITfEditSession *p){return InterlockedIncrement(&((Edit*)p)->refs);}
static BOOL edit_target_current(Edit *e) {
    return GetForegroundWindow()==e->window && GetFocus()==e->control;
}
static void edit_ack(Edit *e,uint32_t method) {
    if(e->notified) return;
    e->notified=TRUE;
    if(e->service->focus_epoch==e->epoch && e->service->pipe!=INVALID_HANDLE_VALUE) {
        // An asynchronous edit can outlive a focus change within the same window.
        if(method==4 && !edit_target_current(e)) {
            disconnect(e->service);
            return;
        }
        Reply ignored;
        exchange(e->service,method,0,0,&ignored);
    }
}
static ULONG STDMETHODCALLTYPE edit_drop(ITfEditSession *p) {
    Edit *e=(Edit*)p;
    LONG n=InterlockedDecrement(&e->refs);
    if(!n) {
        if(e->caret && e->service->focus_epoch==e->epoch) e->service->caret_pending=FALSE;
        edit_ack(e,4);
        ITfContext_Release(e->context);
        drop(e->service);
        HeapFree(GetProcessHeap(),0,e);
        InterlockedDecrement(&objects);
    }
    return n;
}
static void trace_result(const char *label,HRESULT a,HRESULT b) {
    if(GetEnvironmentVariableW(L"CONTEXTIME_TSF_TRACE",NULL,0)==0) return;
    WCHAR path[MAX_PATH];
    DWORD n=GetTempPathW(MAX_PATH,path);
    if(!n||n>MAX_PATH-40) return;
    lstrcatW(path,L"contextime-tsf-result.txt");
    HANDLE file=CreateFileW(path,FILE_APPEND_DATA,FILE_SHARE_READ|FILE_SHARE_WRITE,NULL,OPEN_ALWAYS,0,NULL);
    if(file==INVALID_HANDLE_VALUE) return;
    char line[96];
    int count=wsprintfA(line,"%s %08lx %08lx\r\n",label,(LONG)a,(LONG)b);
    DWORD wrote;WriteFile(file,line,count,&wrote,NULL);CloseHandle(file);
}
static HRESULT start_commit_composition(ITfContext *context,TfEditCookie cookie,
                                        ITfComposition **composition,ITfRange **range) {
    ITfContextComposition *manager=NULL;
    HRESULT hr=ITfContext_QueryInterface(context,&IID_ITfContextComposition,(void**)&manager);
    if(FAILED(hr)) return hr;

    TF_SELECTION selection={0};
    ULONG fetched=0;
    hr=ITfContext_GetSelection(context,cookie,TF_DEFAULT_SELECTION,1,&selection,&fetched);
    if(hr==S_OK) {
        if(fetched==1 && selection.range) {
            hr=ITfContextComposition_StartComposition(manager,cookie,selection.range,NULL,composition);
            if(hr==S_OK && !*composition) hr=TF_E_COMPOSITION_REJECTED;
            if(hr==S_OK) hr=ITfComposition_GetRange(*composition,range);
            if(hr==S_OK && !*range) hr=E_UNEXPECTED;
        } else {
            hr=TS_E_NOSELECTION;
        }
    }
    if(selection.range) ITfRange_Release(selection.range);
    ITfContextComposition_Release(manager);
    return hr;
}

static BOOL unsupported_insert(HRESULT hr) {
    return hr==E_NOINTERFACE || hr==E_NOTIMPL || hr==TF_E_COMPOSITION_REJECTED || hr==E_INVALIDARG;
}

static BOOL rejected_before_write(HRESULT hr) {
    return unsupported_insert(hr) || hr==TF_E_NOLOCK || hr==TF_E_DISCONNECTED ||
        hr==TS_E_NOSELECTION || hr==TS_E_READONLY || hr==TF_E_RANGE_NOT_COVERED;
}

static HRESULT STDMETHODCALLTYPE edit_apply(ITfEditSession *p,TfEditCookie cookie) {
    Edit *e=(Edit*)p;
    if(e->service->focus_epoch!=e->epoch || !edit_target_current(e)) {
        edit_ack(e,4);
        return E_ABORT;
    }
    ITfComposition *composition=NULL;
    ITfRange *range=NULL;
    ITfInsertAtSelection *insert=NULL;
    BOOL attempted_write=FALSE;
    HRESULT hr=ITfContext_QueryInterface(e->context,&IID_ITfInsertAtSelection,(void**)&insert);
    if(hr==S_OK && insert) {
        attempted_write=TRUE;
        hr=ITfInsertAtSelection_InsertTextAtSelection(insert,cookie,0,e->reply.text,e->reply.length,&range);
        ITfInsertAtSelection_Release(insert);
    } else if(hr==S_OK) {
        hr=E_NOINTERFACE;
    }
    HRESULT direct_result=hr;
    if(unsupported_insert(hr)) {
        if(range) { ITfRange_Release(range); range=NULL; }
        hr=start_commit_composition(e->context,cookie,&composition,&range);
        attempted_write=hr==S_OK;
        if(attempted_write) {
            hr=ITfRange_SetText(range,cookie,0,e->reply.text,e->reply.length);
        }
    }
    if(hr==S_OK && range && SUCCEEDED(ITfRange_Collapse(range,cookie,TF_ANCHOR_END))) {
        TF_SELECTION selection={range,{TF_AE_NONE,FALSE}};
        ITfContext_SetSelection(e->context,cookie,1,&selection);
    }
    BOOL possible_write=attempted_write && !rejected_before_write(hr);
    if(composition) {
        HRESULT end_result=ITfComposition_EndComposition(composition,cookie);
        ITfComposition_Release(composition);
        if(hr==S_OK) hr=end_result;
    }
    if(range) ITfRange_Release(range);
    // Replay only if the text store rejected the write before changing the document.
    edit_ack(e,hr==S_OK ? 3 : possible_write ? 8 : 4);
    trace_result("apply",direct_result,hr);
    return hr;
}
static ITfEditSessionVtbl edit_vtbl={edit_query,edit_add,edit_drop,edit_apply};

static BOOL visible_text_bounds(ITfContextView *view,TfEditCookie cookie,ITfRange *range,RECT *bounds) {
    BOOL clipped=FALSE;
    return ITfContextView_GetTextExt(view,cookie,range,bounds,&clipped)==S_OK
        && !clipped && bounds->bottom>bounds->top && bounds->right>=bounds->left;
}

static BOOL caret_bounds(ITfContextView *view,TfEditCookie cookie,ITfRange *range,RECT *bounds) {
    if(visible_text_bounds(view,cookie,range,bounds)) return TRUE;

    // Some text stores return no rectangle for a collapsed range. Use the
    // leading edge of the next character, or the trailing edge of the previous.
    LONG moved=0;
    if(SUCCEEDED(ITfRange_ShiftEnd(range,cookie,1,&moved,NULL)) && moved==1) {
        if(visible_text_bounds(view,cookie,range,bounds)) return TRUE;
        if(FAILED(ITfRange_Collapse(range,cookie,TF_ANCHOR_START))) return FALSE;
    }
    if(SUCCEEDED(ITfRange_ShiftStart(range,cookie,-1,&moved,NULL)) && moved==-1
       && visible_text_bounds(view,cookie,range,bounds)) {
        bounds->left=bounds->right;
        return TRUE;
    }
    return FALSE;
}

static HRESULT STDMETHODCALLTYPE caret_apply(ITfEditSession *p,TfEditCookie cookie) {
    Edit *e=(Edit*)p;
    Service *s=e->service;
    if(s->focus_epoch!=e->epoch) return S_OK;
    if(!edit_target_current(e) || s->pipe==INVALID_HANDLE_VALUE) return S_OK;

    TF_SELECTION selection={0};
    ULONG fetched=0;
    ITfContextView *view=NULL;
    RECT rect={0};
    HRESULT hr=ITfContext_GetSelection(e->context,cookie,TF_DEFAULT_SELECTION,1,&selection,&fetched);
    if(hr==S_OK && fetched==1 && selection.range) {
        TfAnchor edge=selection.style.ase==TF_AE_START ? TF_ANCHOR_START : TF_ANCHOR_END;
        hr=ITfRange_Collapse(selection.range,cookie,edge);
        if(SUCCEEDED(hr)) hr=ITfContext_GetActiveView(e->context,&view);
        if(SUCCEEDED(hr) && view && caret_bounds(view,cookie,selection.range,&rect) && s->focus_epoch==e->epoch) {
            if(!s->caret_reported || s->caret_control!=e->control || !EqualRect(&s->caret_bounds,&rect)) {
                Request req={7,(uint32_t)rect.left,(uint32_t)rect.top,(uint32_t)rect.bottom};
                Reply reply;
                if(exchange_request(s,req,&reply) && reply.handled) {
                    s->caret_bounds=rect;
                    s->caret_control=e->control;
                    s->caret_reported=TRUE;
                }
            }
        }
    }
    if(view) ITfContextView_Release(view);
    if(selection.range) ITfRange_Release(selection.range);
    return S_OK;
}
static ITfEditSessionVtbl caret_vtbl={edit_query,edit_add,edit_drop,caret_apply};

static void request_caret(Service *s,ITfContext *context) {
    if(s->caret_pending || s->pipe==INVALID_HANDLE_VALUE) return;
    Edit *e=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(*e));
    if(!e) return;
    e->iface.lpVtbl=&caret_vtbl;
    e->refs=1;
    e->context=context;
    e->service=s;
    e->epoch=s->focus_epoch;
    e->window=GetForegroundWindow();
    e->control=GetFocus();
    e->notified=TRUE;
    e->caret=TRUE;
    retain(s);
    ITfContext_AddRef(context);
    InterlockedIncrement(&objects);
    s->caret_pending=TRUE;
    HRESULT result=E_FAIL;
    ITfContext_RequestEditSession(context,s->client,&e->iface,TF_ES_ASYNCDONTCARE|TF_ES_READ,&result);
    ITfEditSession_Release(&e->iface);
}

static HRESULT STDMETHODCALLTYPE layout_change(ITfTextLayoutSink *p,ITfContext *context,TfLayoutCode code,ITfContextView *view) {
    Service *s=OWNER(p,layout);
    if(code==TF_LC_CHANGE || code==TF_LC_CREATE) request_caret(s,context);
    if(code==TF_LC_DESTROY) s->caret_reported=FALSE;
    return S_OK;
}
static ITfTextLayoutSinkVtbl layout_vtbl={layout_query,layout_add,layout_drop,layout_change};

static void track_caret(Service *s,ITfContext *context) {
    if(!s->layout_source) {
        ITfSource *source=NULL;
        if(SUCCEEDED(ITfContext_QueryInterface(context,&IID_ITfSource,(void**)&source))) {
            if(SUCCEEDED(ITfSource_AdviseSink(source,&text_layout_sink_id,(IUnknown*)&s->layout,&s->layout_cookie))) {
                s->layout_source=source;
            } else {
                ITfSource_Release(source);
            }
        }
    }
    request_caret(s,context);
}

static const GUID toggle_shift_id={0x58582784,0x37cd,0x4c61,{0xae,0xcd,0x16,0xc3,0xae,0x48,0x3c,0xdc}};
static const TF_PRESERVEDKEY toggle_shift_key={VK_SHIFT,TF_MOD_ON_KEYUP};

static uint32_t input_key(Service *s, ITfContext *context, WPARAM key, LPARAM flags) {
    // KEYEVENTF_UNICODE is used by the backend's deferred commit fallback.
    // Treating VK_PACKET as source text would translate the result again.
    if (key==VK_PACKET) return 0;
    if (!context || (GetKeyState(VK_MENU)&0x8000) ||
        (GetKeyState(VK_LWIN)&0x8000) || (GetKeyState(VK_RWIN)&0x8000)) return FALSE;
    TF_STATUS status;
    if (SUCCEEDED(ITfContext_GetStatus(context,&status)) && (status.dwDynamicFlags & TF_SD_READONLY)) return FALSE;
    if (GetKeyState(VK_CONTROL)&0x8000) {
        if (s->translation_source && key=='V' && !(GetKeyState(VK_SHIFT)&0x8000)) return EDIT_KEY_BASE+'V';
        return shortcut_key(key) && !(GetKeyState(VK_SHIFT)&0x8000) ? EDIT_KEY_BASE+(uint32_t)key : 0;
    }
    if ((key==VK_LEFT || key==VK_RIGHT) && (GetKeyState(VK_SHIFT)&0x8000)) return EDIT_KEY_BASE+(uint32_t)key;
    if (key==VK_SPACE && !s->buffered_source && (GetKeyState(VK_SHIFT)&0x8000)) return EDIT_KEY_BASE+VK_SPACE;
    BOOL uppercase=(GetKeyState(VK_SHIFT)&0x8000)!=0;
    if (s->buffered_source) uppercase=uppercase!=((GetKeyState(VK_CAPITAL)&1)!=0);
    if (!s->buffered_source && key>='A' && key<='Z' && uppercase) return SYMBOL_KEY_BASE+(uint32_t)key;
    if ((!s->buffered_source && key>='A' && key<='Z') || key==VK_RETURN || key==VK_BACK ||
        key==VK_ESCAPE || key==VK_SPACE || key==VK_LEFT || key==VK_RIGHT ||
        key==VK_UP || key==VK_DOWN || key==VK_PRIOR || key==VK_NEXT || key==VK_DELETE) return (uint32_t)key;

    if (!s->buffered_source && key>='0' && key<='9' && !(GetKeyState(VK_SHIFT)&0x8000)) return (uint32_t)key;
    if (!s->buffered_source && !((key>='0' && key<='9') || (key>=VK_MULTIPLY && key<=VK_DIVIDE) ||
          (key>=VK_OEM_1 && key<=VK_OEM_3) || (key>=VK_OEM_4 && key<=VK_OEM_8) ||
          key==VK_OEM_102)) return 0;

    BYTE state[256];
    WCHAR chars[4];
    const UINT preserve_keyboard_state=4;
    if (!GetKeyboardState(state) ||
        ToUnicodeEx((UINT)key,((UINT)flags>>16)&0xff,state,chars,4,
                    preserve_keyboard_state,GetKeyboardLayout(0))!=1) return 0;
    WCHAR c=chars[0];
    if (s->buffered_source && key>='A' && key<='Z' &&
        ((c>='a' && c<='z') || (c>='A' && c<='Z'))) {
        c=(WCHAR)(c|0x20);
        if (uppercase) c-=L'a'-L'A';
    }
    if (s->buffered_source && c>=0x20 && c!=0x7f && !(c>=0x80 && c<=0x9f) &&
        !(c>=0xd800 && c<=0xdfff)) {
        if (c>=L'a' && c<=L'z') return c-L'a'+'A';
        return SYMBOL_KEY_BASE+c;
    }
    if ((c>='!' && c<='/') || (c>=':' && c<='@') ||
        (c>='[' && c<='`') || (c>='{' && c<='~')) return SYMBOL_KEY_BASE+c;
    return 0;
}
static HRESULT STDMETHODCALLTYPE key_focus(ITfKeyEventSink *p,BOOL active) {
    Service *s=OWNER(p,keys);
    trace_result("shortcut-key-focus",active,GetCurrentThreadId());
    if (active) {
        if (s->shift_hook) shift_service=s;
        refresh_input_mode(s);
    } else {
        if (shift_service==s) shift_service=NULL;
        reset_shift(s);
        ZeroMemory(s->shortcut_down,sizeof(s->shortcut_down));
        disconnect(s);
    }
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE test_down(ITfKeyEventSink *p,ITfContext *context,WPARAM key,LPARAM flags,BOOL *eaten) {
    Service *s=OWNER(p,keys);
    Reply reply;
    *eaten=FALSE;
    refresh_caps_lock(s);
    if(is_shift(key) || shortcut_key(key)) trace_result("shortcut-test-down",(HRESULT)key,(HRESULT)flags);
    track_shift_down(s,key,flags);
    if (is_shift(key)) return S_OK;
    if (key==VK_PACKET) return S_OK;
    if (key==VK_CAPITAL) {
        if (!s->buffered_source) disconnect(s);
        return S_OK;
    }
    if (shortcut_held(s,key)) { *eaten=TRUE; return S_OK; }
    uint32_t input=input_key(s,context,key,flags);
    BOOL shortcut=shortcut_key(key) && input==EDIT_KEY_BASE+key;
    if (shortcut && (flags&(1L<<30))) return S_OK;
    if ((s->english || (s->caps_lock && !s->buffered_source)) && !shortcut) return S_OK;
    if (!input) {
        // Ignore modifier transitions; other passed-through keys invalidate the draft.
        if (!(GetKeyState(VK_CONTROL)&0x8000) &&
            key!=VK_CONTROL && key!=VK_LCONTROL && key!=VK_RCONTROL &&
            key!=VK_MENU && key!=VK_LMENU && key!=VK_RMENU &&
            key!=VK_LWIN && key!=VK_RWIN) disconnect(s);
        return S_OK;
    }
    if (key==VK_RETURN && s->enter_down && (flags&(1L<<30))) { *eaten=TRUE; return S_OK; }
    *eaten=exchange(s,1,input,flags,&reply) && reply.handled;
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE key_down(ITfKeyEventSink *p,ITfContext *context,WPARAM key,LPARAM flags,BOOL *eaten) {
    Service *s=OWNER(p,keys);
    Reply reply;
    track_shift_down(s,key,flags);
    *eaten=shortcut_held(s,key);
    if (*eaten) return S_OK;
    uint32_t input=input_key(s,context,key,flags);
    if (shortcut_key(key) && input==EDIT_KEY_BASE+key) {
        *eaten=press_shortcut(s,key,flags);
        return S_OK;
    }
    if (!input || s->english || (s->caps_lock && !s->buffered_source)) return S_OK;
    if (key==VK_RETURN && s->enter_down && (flags&(1L<<30))) { *eaten=TRUE; return S_OK; }
    if (key!=VK_ESCAPE) track_caret(s,context);
    if (!exchange(s,2,input,flags,&reply) || !reply.handled) return S_OK;
    *eaten=TRUE;
    if (key==VK_RETURN) s->enter_down=TRUE;
    if (reply.length) {
        HRESULT result=E_FAIL;
        HRESULT hr=E_OUTOFMEMORY;
        Edit *e=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(*e));
        if(e){
            e->iface.lpVtbl=&edit_vtbl;e->refs=1;e->context=context;e->reply=reply;
            e->service=s;e->epoch=s->focus_epoch;e->window=GetForegroundWindow();
            e->control=GetFocus();
            retain(s);
            ITfContext_AddRef(context);InterlockedIncrement(&objects);
            hr=ITfContext_RequestEditSession(context,s->client,&e->iface,TF_ES_ASYNCDONTCARE|TF_ES_READWRITE,&result);
            trace_result("request",hr,result);
            if(FAILED(hr)||FAILED(result)) edit_ack(e,4);
            ITfEditSession_Release(&e->iface);
        } else {
            Reply ignored;
            exchange(s,4,0,0,&ignored);
        }
    }
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE test_up(ITfKeyEventSink *p,ITfContext *c,WPARAM key,LPARAM l,BOOL *eaten) {
    Service *s=OWNER(p,keys);
    if (!is_shift(key) && s->shift_down) trace_result("shift-ignore-testup",(HRESULT)key,(HRESULT)l);
    if(is_shift(key) || shortcut_key(key)) trace_result("shortcut-test-up",(HRESULT)key,(HRESULT)l);
    if (key==VK_CAPITAL) refresh_caps_lock(s);
    *eaten=(key==VK_RETURN && s->enter_down) || shortcut_held(s,key) ||
        (is_shift(key) && s->shift_alone);
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE key_up(ITfKeyEventSink *p,ITfContext *c,WPARAM key,LPARAM l,BOOL *eaten) {
    test_up(p,c,key,l,eaten);
    Service *s=OWNER(p,keys);
    if (key==VK_RETURN) s->enter_down=FALSE;
    release_shortcut(s,key);
    track_shift_up(s,key,l);
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE preserved(ITfKeyEventSink *p,ITfContext *c,REFGUID id,BOOL *eaten) {
    Service *s=OWNER(p,keys);
    *eaten=IsEqualGUID(id,&toggle_shift_id) && finish_shift(s);
    if(IsEqualGUID(id,&toggle_shift_id)) trace_result("shortcut-preserved-shift",*eaten,0);
    if(IsEqualGUID(id,&toggle_comma_id) && c) {
        TF_STATUS status={0};
        if(ITfContext_GetStatus(c,&status)==S_OK && !(status.dwDynamicFlags&TF_SD_READONLY)) {
            *eaten=press_shortcut(s,VK_OEM_COMMA,0);
        }
        trace_result("shortcut-preserved-comma",*eaten,0);
    }
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE doc_event(ITfThreadMgrEventSink *p,ITfDocumentMgr *d){return S_OK;}
static HRESULT STDMETHODCALLTYPE focus_event(ITfThreadMgrEventSink *p,ITfDocumentMgr *now,ITfDocumentMgr *old) {
    if (now!=old) {
        Service *s=OWNER(p,focus);
        reset_shift(s);
        ZeroMemory(s->shortcut_down,sizeof(s->shortcut_down));
        disconnect(s);
        if (now) refresh_input_mode(s);
    }
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE context_event(ITfThreadMgrEventSink *p,ITfContext *c){Service *s=OWNER(p,focus);reset_shift(s);disconnect(s);return S_OK;}
static ITfKeyEventSinkVtbl key_vtbl={keys_query,keys_add,keys_drop,key_focus,test_down,test_up,key_down,key_up,preserved};
static ITfThreadMgrEventSinkVtbl focus_vtbl={focus_query,focus_add,focus_drop,doc_event,doc_event,focus_event,context_event,context_event};

static HRESULT STDMETHODCALLTYPE deactivate(ITfTextInputProcessor *p) {
    Service *s=OWNER(p,tip);
    reset_shift(s);
    if (shift_service==s) shift_service=NULL;
    if (s->shift_hook) {
        UnhookWindowsHookEx(s->shift_hook);
        s->shift_hook=NULL;
    }
    disconnect(s);
    if(s->bar) {
        ITfLangBarItemMgr_RemoveItem(s->bar,(ITfLangBarItem*)&s->mode_button);
        ITfLangBarItemMgr_Release(s->bar);
        s->bar=NULL;
    }
    release_bar_sink(s);
    if(s->keymgr){
        if(s->shift_preserved) {
            ITfKeystrokeMgr_UnpreserveKey(s->keymgr,&toggle_shift_id,&toggle_shift_key);
            s->shift_preserved=FALSE;
        }
        if(s->comma_preserved) {
            ITfKeystrokeMgr_UnpreserveKey(s->keymgr,&toggle_comma_id,&toggle_comma_key);
            s->comma_preserved=FALSE;
        }
        ITfKeystrokeMgr_UnadviseKeyEventSink(s->keymgr,s->client);
        ITfKeystrokeMgr_Release(s->keymgr);
        s->keymgr=NULL;
    }
    if(s->source){if(s->focus_cookie!=TF_INVALID_COOKIE) ITfSource_UnadviseSink(s->source,s->focus_cookie);ITfSource_Release(s->source);s->source=NULL;}
    if(s->manager) { ITfThreadMgr_Release(s->manager); s->manager=NULL; }
    s->focus_cookie=TF_INVALID_COOKIE;
    return S_OK;
}
static HRESULT STDMETHODCALLTYPE activate(ITfTextInputProcessor *p,ITfThreadMgr *manager,TfClientId id) {
    Service *s=OWNER(p,tip);
    s->client=id;
    s->manager=manager;
    ITfThreadMgr_AddRef(manager);
    HRESULT hr=ITfThreadMgr_QueryInterface(manager,&IID_ITfKeystrokeMgr,(void**)&s->keymgr);
    if (SUCCEEDED(hr)) hr=ITfThreadMgr_QueryInterface(manager,&IID_ITfSource,(void**)&s->source);
    if (SUCCEEDED(hr)) hr=ITfSource_AdviseSink(s->source,&IID_ITfThreadMgrEventSink,(IUnknown*)&s->focus,&s->focus_cookie);
    if (SUCCEEDED(hr)) hr=ITfKeystrokeMgr_AdviseKeyEventSink(s->keymgr,id,&s->keys,TRUE);
    if (FAILED(hr)) { deactivate(p); return hr; }
    s->shift_hook=SetWindowsHookExW(WH_GETMESSAGE,shift_message,NULL,GetCurrentThreadId());
    trace_result("shortcut-hook-install",s->shift_hook?0:GetLastError(),GetCurrentThreadId());
    if (s->shift_hook) shift_service=s;
    refresh_input_mode(s);
    if(SUCCEEDED(ITfThreadMgr_QueryInterface(manager,&IID_ITfLangBarItemMgr,(void**)&s->bar))) {
        if(FAILED(ITfLangBarItemMgr_AddItem(s->bar,(ITfLangBarItem*)&s->mode_button))) {
            ITfLangBarItemMgr_Release(s->bar);
            s->bar=NULL;
        }
    }
    s->shift_preserved=SUCCEEDED(ITfKeystrokeMgr_PreserveKey(s->keymgr,id,&toggle_shift_id,&toggle_shift_key,NULL,0));
    s->comma_preserved=SUCCEEDED(ITfKeystrokeMgr_PreserveKey(s->keymgr,id,&toggle_comma_id,&toggle_comma_key,NULL,0));
    ITfCompartmentMgr *cm=NULL;
    if (SUCCEEDED(ITfThreadMgr_QueryInterface(manager,&IID_ITfCompartmentMgr,(void**)&cm))) {
        ITfCompartment *comp=NULL;
        if (SUCCEEDED(ITfCompartmentMgr_GetCompartment(cm,&GUID_COMPARTMENT_KEYBOARD_OPENCLOSE,&comp))) {
            VARIANT v={0}; V_VT(&v)=VT_I4; V_I4(&v)=1;
            ITfCompartment_SetValue(comp,id,&v);
            ITfCompartment_Release(comp);
        }
        ITfCompartmentMgr_Release(cm);
    }
    return S_OK;
}
static ITfTextInputProcessorVtbl tip_vtbl={tip_query,tip_add,tip_drop,activate,deactivate};

static ULONG STDMETHODCALLTYPE factory_add(IClassFactory *p){InterlockedIncrement(&objects);return 2;}
static ULONG STDMETHODCALLTYPE factory_drop(IClassFactory *p){InterlockedDecrement(&objects);return 1;}
static HRESULT STDMETHODCALLTYPE factory_query(IClassFactory *p,REFIID id,void **out) {
    if(!out) return E_POINTER;
    *out=NULL;
    if(!IsEqualGUID(id,&IID_IUnknown) && !IsEqualGUID(id,&IID_IClassFactory)) return E_NOINTERFACE;
    *out=p;factory_add(p);return S_OK;
}
static HRESULT STDMETHODCALLTYPE factory_create(IClassFactory *p,IUnknown *outer,REFIID id,void **out) {
    if(!out) return E_POINTER;
    *out=NULL;
    if(outer) return CLASS_E_NOAGGREGATION;
    Service *s=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(*s));
    if(!s) return E_OUTOFMEMORY;
    s->refs=1; s->tip.lpVtbl=&tip_vtbl; s->keys.lpVtbl=&key_vtbl; s->focus.lpVtbl=&focus_vtbl; s->layout.lpVtbl=&layout_vtbl;
    s->mode_button.lpVtbl=&mode_button_vtbl; s->mode_source.lpVtbl=&mode_source_vtbl;
    s->pipe=INVALID_HANDLE_VALUE; s->focus_cookie=TF_INVALID_COOKIE;
    InterlockedIncrement(&objects);
    HRESULT hr=query(s,id,out); drop(s); return hr;
}
static HRESULT STDMETHODCALLTYPE factory_lock(IClassFactory *p,BOOL lock){if(lock) factory_add(p);else factory_drop(p);return S_OK;}
static IClassFactoryVtbl factory_vtbl={factory_query,factory_add,factory_drop,factory_create,factory_lock};
static IClassFactory factory={&factory_vtbl};

__declspec(dllexport) HRESULT WINAPI DllGetClassObject(REFCLSID cls,REFIID id,void **out) {
    if(!out) return E_POINTER;
    *out=NULL;
    if(!IsEqualGUID(cls,&service_id)) return CLASS_E_CLASSNOTAVAILABLE;
    return factory_query(&factory,id,out);
}
__declspec(dllexport) HRESULT WINAPI DllCanUnloadNow(void){return objects?S_FALSE:S_OK;}
BOOL WINAPI DllMain(HINSTANCE instance,DWORD reason,LPVOID reserved) {
    if(reason==DLL_PROCESS_ATTACH){module=instance; DisableThreadLibraryCalls(instance);}
    return TRUE;
}

static HRESULT configure(BOOL remove) {
    HRESULT init=CoInitializeEx(NULL,COINIT_APARTMENTTHREADED);
    if(FAILED(init) && init!=RPC_E_CHANGED_MODE) return init;
    ITfInputProcessorProfiles *profiles=NULL;
    ITfCategoryMgr *categories=NULL;
    HRESULT hr=CoCreateInstance(&CLSID_TF_InputProcessorProfiles,NULL,CLSCTX_INPROC_SERVER,
                               &IID_ITfInputProcessorProfiles,(void**)&profiles);
    if(SUCCEEDED(hr)) hr=CoCreateInstance(&CLSID_TF_CategoryMgr,NULL,CLSCTX_INPROC_SERVER,
                               &IID_ITfCategoryMgr,(void**)&categories);
    if(SUCCEEDED(hr) && remove) {
        ITfCategoryMgr_UnregisterCategory(categories,&service_id,&tray_support,&service_id);
        ITfCategoryMgr_UnregisterCategory(categories,&service_id,&GUID_TFCAT_TIP_KEYBOARD,&service_id);
        ITfInputProcessorProfiles_RemoveLanguageProfile(profiles,&service_id,0x0804,&profile_id);
        hr=ITfInputProcessorProfiles_Unregister(profiles,&service_id);
        LONG rc=RegDeleteKeyW(HKEY_LOCAL_MACHINE,server_key);
        if(rc!=ERROR_SUCCESS && rc!=ERROR_FILE_NOT_FOUND) hr=HRESULT_FROM_WIN32(rc);
        rc=RegDeleteKeyW(HKEY_LOCAL_MACHINE,clsid_key);
        if(rc!=ERROR_SUCCESS && rc!=ERROR_FILE_NOT_FOUND) hr=HRESULT_FROM_WIN32(rc);
    } else if(SUCCEEDED(hr)) {
        HKEY key=NULL;
        WCHAR path[32768];
        DWORD n=GetModuleFileNameW(module,path,32768);
        LONG rc=(n>0 && n<32768)?RegCreateKeyExW(HKEY_LOCAL_MACHINE,server_key,0,NULL,0,KEY_SET_VALUE,NULL,&key,NULL):ERROR_BAD_PATHNAME;
        if(rc==ERROR_SUCCESS) rc=RegSetValueExW(key,NULL,0,REG_SZ,(const BYTE*)path,(n+1)*sizeof(WCHAR));
        static const WCHAR apartment[]=L"Apartment";
        if(rc==ERROR_SUCCESS) rc=RegSetValueExW(key,L"ThreadingModel",0,REG_SZ,(const BYTE*)apartment,sizeof(apartment));
        if(key) RegCloseKey(key);
        hr=HRESULT_FROM_WIN32(rc);
        static const WCHAR name[]=L"Eternally";
        if(SUCCEEDED(hr)) hr=ITfInputProcessorProfiles_Register(profiles,&service_id);
        if(SUCCEEDED(hr)) hr=ITfCategoryMgr_RegisterCategory(categories,&service_id,&GUID_TFCAT_TIP_KEYBOARD,&service_id);
        if(SUCCEEDED(hr)) hr=ITfCategoryMgr_RegisterCategory(categories,&service_id,&tray_support,&service_id);
        if(SUCCEEDED(hr)) hr=ITfInputProcessorProfiles_AddLanguageProfile(profiles,&service_id,0x0804,&profile_id,name,ARRAYSIZE(name)-1,path,n,0);
        if(SUCCEEDED(hr)) hr=ITfInputProcessorProfiles_EnableLanguageProfileByDefault(profiles,&service_id,0x0804,&profile_id,TRUE);
    }
    if(categories) ITfCategoryMgr_Release(categories);
    if(profiles) ITfInputProcessorProfiles_Release(profiles);
    if(SUCCEEDED(init)) CoUninitialize();
    return hr;
}
__declspec(dllexport) HRESULT WINAPI DllRegisterServer(void){return configure(FALSE);}
__declspec(dllexport) HRESULT WINAPI DllUnregisterServer(void){return configure(TRUE);}
