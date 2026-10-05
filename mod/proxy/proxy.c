// version.dll proxy for Plex RPC.
//
// Plex (a Qt application) loads the Windows library version.dll from its own
// program folder before the system folder. This file builds a drop-in
// version.dll that:
//
//   1. forwards every real version.dll function to the genuine library in
//      C:\Windows\System32, so Plex keeps working exactly as before, and
//   2. starts the Plex RPC engine (plexrpc_core.dll) on a background thread
//      when Plex loads it.
//
// It is a transparent proxy: it adds behaviour, it does not change or hide
// anything Plex does. Remove the two files to uninstall.

#include <windows.h>

// --- Forwarding to the real version.dll ------------------------------------
//
// The real functions are resolved once at load time. Each exported name is a
// tiny stub that jumps to the real function, so the original signatures do
// not matter.

static FARPROC real[17];

// ret_zero returns 0 and does nothing; used as a safe fallback.
__attribute__((naked)) static void ret_zero(void) {
    __asm__ __volatile__("xor %eax, %eax\n\tret");
}

static const char *kNames[17] = {
    "GetFileVersionInfoA",        "GetFileVersionInfoByHandle",
    "GetFileVersionInfoExA",      "GetFileVersionInfoExW",
    "GetFileVersionInfoSizeA",    "GetFileVersionInfoSizeExA",
    "GetFileVersionInfoSizeExW",  "GetFileVersionInfoSizeW",
    "GetFileVersionInfoW",        "VerFindFileA",
    "VerFindFileW",               "VerInstallFileA",
    "VerInstallFileW",            "VerLanguageNameA",
    "VerLanguageNameW",           "VerQueryValueA",
    "VerQueryValueW",
};

static void load_real_version(void) {
    char path[MAX_PATH];
    UINT n = GetSystemDirectoryA(path, MAX_PATH);
    if (n == 0 || n > MAX_PATH - 12) return;
    lstrcatA(path, "\\version.dll");
    HMODULE h = LoadLibraryA(path);
    if (!h) return;
    for (int i = 0; i < 17; i++) {
        real[i] = GetProcAddress(h, kNames[i]);
        // If a rarely-used export is missing on this Windows build, point it
        // at a harmless stub so a call can never jump to a null address.
        if (!real[i]) real[i] = (FARPROC)ret_zero;
    }
}

// Each export is a naked stub: jump to the resolved real function. On x64 the
// arguments are already in place, so a plain jump forwards the call untouched.
// Stubs use internal names (proxy_*) and are exported under the real names
// via version.def, to avoid clashing with the prototypes in winver.h.
#define STUB(idx, name)                                                        \
    __attribute__((naked)) void proxy_##name(void) {                           \
        __asm__ __volatile__("jmp *%0" : : "m"(real[idx]));                    \
    }

STUB(0, GetFileVersionInfoA)
STUB(1, GetFileVersionInfoByHandle)
STUB(2, GetFileVersionInfoExA)
STUB(3, GetFileVersionInfoExW)
STUB(4, GetFileVersionInfoSizeA)
STUB(5, GetFileVersionInfoSizeExA)
STUB(6, GetFileVersionInfoSizeExW)
STUB(7, GetFileVersionInfoSizeW)
STUB(8, GetFileVersionInfoW)
STUB(9, VerFindFileA)
STUB(10, VerFindFileW)
STUB(11, VerInstallFileA)
STUB(12, VerInstallFileW)
STUB(13, VerLanguageNameA)
STUB(14, VerLanguageNameW)
STUB(15, VerQueryValueA)
STUB(16, VerQueryValueW)

// --- Starting the engine ---------------------------------------------------

typedef void (*start_fn)(void);

// path_next_to_this builds a path to a file sitting beside this DLL.
static int path_next_to_this(const char *name, char *out, DWORD cap) {
    HMODULE self = NULL;
    if (!GetModuleHandleExA(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS |
                                GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
                            (LPCSTR)&path_next_to_this, &self))
        return 0;
    DWORD n = GetModuleFileNameA(self, out, cap);
    if (n == 0 || n >= cap) return 0;
    for (DWORD i = n; i > 0; i--) {
        if (out[i - 1] == '\\' || out[i - 1] == '/') {
            out[i] = 0;
            break;
        }
    }
    lstrcatA(out, name);
    return 1;
}

static DWORD WINAPI start_engine(LPVOID unused) {
    (void)unused;
    char dll[MAX_PATH];
    if (!path_next_to_this("plexrpc_core.dll", dll, MAX_PATH)) return 0;
    HMODULE core = LoadLibraryA(dll);
    if (!core) return 0;
    start_fn start = (start_fn)GetProcAddress(core, "PlexRpcStart");
    if (start) start();
    return 0;
}

BOOL WINAPI DllMain(HINSTANCE inst, DWORD reason, LPVOID reserved) {
    (void)reserved;
    if (reason == DLL_PROCESS_ATTACH) {
        DisableThreadLibraryCalls(inst);
        load_real_version();
        // Do real work on a new thread; DllMain itself must stay minimal.
        HANDLE t = CreateThread(NULL, 0, start_engine, NULL, 0, NULL);
        if (t) CloseHandle(t);
    }
    return TRUE;
}
