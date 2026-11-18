// Exercise the real TSF/message-hook handlers without a desktop, backend pipe,
// registry changes, or keyboard injection. Queue key state is controlled here.
#define COBJMACROS
#include <windows.h>
#include <msctf.h>
#include <assert.h>
#include <stdio.h>

static SHORT keyboard[256];
static unsigned toggle_attempts;

static SHORT WINAPI test_GetKeyState(int key) {
    return key >= 0 && key < 256 ? keyboard[key] : 0;
}
static HANDLE WINAPI test_CreateFileW(LPCWSTR path, DWORD access, DWORD share,
        LPSECURITY_ATTRIBUTES attributes, DWORD creation, DWORD flags, HANDLE template) {
    (void)path; (void)access; (void)share; (void)attributes;
    (void)creation; (void)flags; (void)template;
    toggle_attempts++;
    return INVALID_HANDLE_VALUE;
}
static DWORD WINAPI test_GetEnvironmentVariableW(LPCWSTR key, LPWSTR value, DWORD size) {
    (void)key; (void)value; (void)size;
    return 0;
}
static HWND WINAPI test_GetForegroundWindow(void) { return (HWND)1; }
static DWORD WINAPI test_GetWindowThreadProcessId(HWND window, LPDWORD process) {
    (void)window; (void)process;
    return GetCurrentThreadId();
}

#define GetKeyState test_GetKeyState
#define CreateFileW test_CreateFileW
#define GetEnvironmentVariableW test_GetEnvironmentVariableW
#define GetForegroundWindow test_GetForegroundWindow
#define GetWindowThreadProcessId test_GetWindowThreadProcessId
#include "../service.c"

static HRESULT STDMETHODCALLTYPE context_status(ITfContext *self, TF_STATUS *status) {
    (void)self;
    ZeroMemory(status, sizeof(*status));
    return S_OK;
}
static ITfContextVtbl context_vtable = {.GetStatus = context_status};
static ITfContext context = {&context_vtable};
static const LPARAM left_scan = (LPARAM)0x002a0001;
static const LPARAM right_scan = (LPARAM)0x00360001;
static const LPARAM release_flags = (LPARAM)0xc0000000u;

static void queue_shift(BOOL left, BOOL right) {
    keyboard[VK_LSHIFT] = left ? (SHORT)0x8000 : 0;
    keyboard[VK_RSHIFT] = right ? (SHORT)0x8000 : 0;
    keyboard[VK_SHIFT] = left || right ? (SHORT)0x8000 : 0;
}
static void start(Service *service) {
    ZeroMemory(service, sizeof(*service));
    ZeroMemory(keyboard, sizeof(keyboard));
    service->pipe = INVALID_HANDLE_VALUE;
    // Punctuation tests must not try a real backend translation request.
    service->english = TRUE;
    toggle_attempts = 0;
    shift_service = service;
}
static void hook(UINT message, WPARAM key, LPARAM flags) {
    MSG input = {0};
    input.message = message;
    input.wParam = key;
    input.lParam = flags;
    shift_message(HC_ACTION, PM_REMOVE, (LPARAM)&input);
}
static void down(Service *service, WPARAM key, LPARAM flags) {
    BOOL eaten = FALSE;
    test_down(&service->keys, &context, key, flags, &eaten);
}
static void up(Service *service, WPARAM key, LPARAM flags) {
    BOOL eaten = FALSE;
    test_up(&service->keys, &context, key, flags, &eaten);
    key_up(&service->keys, &context, key, flags, &eaten);
}
static BOOL preserved_up(Service *service) {
    BOOL eaten = FALSE;
    preserved(&service->keys, NULL, &toggle_shift_id, &eaten);
    return eaten;
}

static void test_single_tap_and_duplicate_callbacks(void) {
    for (unsigned right = 0; right < 2; right++) {
        Service service;
        LPARAM scan = right ? right_scan : left_scan;
        WPARAM key = right ? VK_RSHIFT : VK_LSHIFT;
        start(&service);
        queue_shift(!right, right);
        down(&service, key, scan);
        hook(WM_KEYDOWN, VK_SHIFT, scan);
        down(&service, key, scan);
        assert(service.shift_alone && !service.shift_chord);

        // Real regression: TSF may send preserved key-up before GetKeyState
        // reflects release. This still completes the tap exactly once.
        assert(preserved_up(&service));
        queue_shift(FALSE, FALSE);
        hook(WM_KEYUP, VK_SHIFT, scan | release_flags);
        up(&service, key, scan | release_flags);
        assert(!preserved_up(&service));
        assert(toggle_attempts == 1 && service.shift_down == 0);
    }
}

static void test_shifted_punctuation_never_rearms(void) {
    Service service;
    start(&service);
    queue_shift(TRUE, FALSE);
    down(&service, VK_SHIFT, left_scan);
    down(&service, '1', 0x00020001);
    hook(WM_KEYDOWN, VK_SHIFT, left_scan);
    down(&service, VK_LSHIFT, left_scan);
    up(&service, '1', release_flags);
    assert(service.shift_chord && !service.shift_alone);
    queue_shift(FALSE, FALSE);
    assert(!preserved_up(&service));
    assert(toggle_attempts == 0);

    // Cancellation belongs to one press, so the following clean tap works.
    queue_shift(FALSE, TRUE);
    down(&service, VK_RSHIFT, right_scan);
    assert(preserved_up(&service));
    assert(toggle_attempts == 1);
}

static void test_unrelated_release_probes_do_not_cancel(void) {
    const WPARAM released[] = {VK_PROCESSKEY, 0, VK_CONTROL, VK_OEM_PERIOD};
    for (unsigned i = 0; i < ARRAYSIZE(released); i++) {
        Service service;
        BOOL eaten = FALSE;
        start(&service);
        queue_shift(TRUE, FALSE);
        down(&service, VK_SHIFT, left_scan);
        queue_shift(FALSE, FALSE);
        // OnTestKeyUp is a query, not proof of a press during this tap.
        test_up(&service.keys, &context, released[i], release_flags, &eaten);
        assert(service.shift_alone && !service.shift_chord);
        up(&service, released[i], release_flags);
        hook(WM_KEYUP, released[i], release_flags);
        assert(preserved_up(&service) && toggle_attempts == 1);
    }
}

static void test_observed_processkey_release(void) {
    Service service;
    start(&service);
    queue_shift(FALSE, TRUE);
    down(&service, VK_SHIFT, right_scan);
    hook(WM_KEYDOWN, VK_SHIFT, right_scan);
    queue_shift(FALSE, FALSE);
    // Observed on the real desktop: TSF substitutes this E5 key-up for the
    // right Shift release before invoking its preserved Shift command.
    up(&service, VK_PROCESSKEY, right_scan | release_flags);
    assert(preserved_up(&service) && toggle_attempts == 1);
}

static void test_character_and_mouse_messages(void) {
    const UINT character_messages[] = {WM_CHAR, WM_SYSCHAR, WM_DEADCHAR, WM_SYSDEADCHAR, WM_UNICHAR};
    const WPARAM control_characters[] = {0, '\t', '\r', 0x1b, 0x7f};
    for (unsigned i = 0; i < ARRAYSIZE(character_messages); i++) {
        for (unsigned j = 0; j < ARRAYSIZE(control_characters); j++) {
            Service service;
            start(&service);
            queue_shift(TRUE, FALSE);
            down(&service, VK_SHIFT, left_scan);
            hook(character_messages[i], control_characters[j], 0);
            assert(preserved_up(&service) && toggle_attempts == 1);
        }
        Service service;
        start(&service);
        queue_shift(TRUE, FALSE);
        down(&service, VK_SHIFT, left_scan);
        hook(character_messages[i], '!', 0);
        hook(WM_KEYDOWN, VK_SHIFT, left_scan);
        assert(!preserved_up(&service) && toggle_attempts == 0);
    }

    const UINT buttons[] = {WM_LBUTTONDOWN, WM_LBUTTONUP, WM_RBUTTONDOWN,
        WM_MBUTTONUP, WM_XBUTTONDOWN, WM_NCLBUTTONUP};
    for (unsigned i = 0; i < ARRAYSIZE(buttons); i++) {
        Service service;
        start(&service);
        queue_shift(TRUE, FALSE);
        down(&service, VK_SHIFT, left_scan);
        hook(buttons[i], 0, 0);
        assert(!preserved_up(&service) && toggle_attempts == 0);
    }
}

static void test_both_sides_and_repeat(void) {
    Service service;
    start(&service);
    queue_shift(TRUE, FALSE);
    down(&service, VK_LSHIFT, left_scan);
    queue_shift(TRUE, TRUE);
    hook(WM_KEYDOWN, VK_SHIFT, right_scan);
    queue_shift(FALSE, TRUE);
    hook(WM_KEYUP, VK_SHIFT, left_scan | release_flags);
    up(&service, VK_LSHIFT, left_scan | release_flags);
    assert(service.shift_down == 2 && !service.shift_alone);
    queue_shift(FALSE, FALSE);
    up(&service, VK_RSHIFT, right_scan | release_flags);
    assert(!preserved_up(&service) && toggle_attempts == 0);

    start(&service);
    queue_shift(TRUE, FALSE);
    down(&service, VK_SHIFT, left_scan | (1L << 30));
    assert(!preserved_up(&service) && toggle_attempts == 0);

    start(&service);
    queue_shift(TRUE, FALSE);
    down(&service, VK_SHIFT, left_scan);
    hook(WM_KEYDOWN, VK_SHIFT, left_scan | (1L << 30));
    assert(preserved_up(&service) && !preserved_up(&service));
    assert(toggle_attempts == 1);
}

int main(void) {
    test_single_tap_and_duplicate_callbacks();
    test_shifted_punctuation_never_rearms();
    test_unrelated_release_probes_do_not_cancel();
    test_observed_processkey_release();
    test_character_and_mouse_messages();
    test_both_sides_and_repeat();
    puts("Native Shift tap/chord regression checks passed.");
    return 0;
}
