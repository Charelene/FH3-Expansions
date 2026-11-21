/* fh3-expansions: lets Azimuth 1.0.4 accept the converted Forza Horizon 3 expansions (Blizzard Mountain and
 * Hot Wheels) without changing azimuth.exe on disk.
 *
 * It is a dinput8.dll proxy. azimuth.exe imports DINPUT8.dll (DirectInput8Create only) and Windows looks in the
 * exe's own folder first, so this DLL is loaded, and its DllMain runs, before azimuth.exe runs any code of its own,
 * and so before Azimuth validates the content folders of its titles (it does that as soon as its launcher opens:
 * FH3-eotest run 2026-11-21 04:32:21 UTC, content lines logged 0.2 s after the session began). Every export
 * forwards to the system's dinput8.dll.
 *
 * What it changes, in memory only (same data as tools/azimuth-dlc's file patch, see main.go there for the
 * addresses and how Azimuth's validator 0x1401a1ba0 uses them): FH3's title record (RVA 0x6c4220 in .rdata) gets
 * a content-package list, +0x7f8 pointer and +0x800 count. A list entry is 0x20 bytes: char* SHA-256 (lower hex)
 * of the package's AppxManifest.xml, char* tree digest, the RVA of the package's product id in the FH3 game image,
 * char* version string. The hashes are not computed here: azimuth-dlc writes them into fh3-expansions.ini next to
 * azimuth.exe when it installs the packages, so Azimuth's own "Incomplete or changed" check still catches a
 * package whose files changed afterwards.
 *
 * Stands down (changes nothing) unless azimuth.exe is the unpatched 1.0.4 build and FH3's list is still empty;
 * an azimuth.exe patched by azimuth-dlc carries its own list. Log: fh3-expansions.log next to azimuth.exe.
 * KERNEL32 only (plus the C runtime). */
#include <windows.h>
#include <stdio.h>
#include <string.h>
#include <wchar.h>

/* azimuth.exe 1.0.4 (SHA-256 56d2c9cb...b092), checked in memory. */
#define AZ_TIMESTAMP 0x6ae393ebu
#define AZ_SIZE_IMAGE 0x8a2000u
#define AZ_SIZE_IMAGE_PATCHED 0x8a3000u /* azimuth-dlc's file patch adds the .fh3dlc section */
#define FH3_RECORD_RVA 0x6c4220u
#define FH3_TITLE_ID_RVA 0x6c1460u /* L"4ce1dfab" */
#define LIST_PTR_OFF 0x7f8
#define LIST_COUNT_OFF 0x800

/* Product id RVAs in forza_durango_release_final.exe 1.0.125.0 (checked by azimuth-dlc against the game exe). */
static const struct {
    const char *identity;
    unsigned product_rva;
} known[] = {
    {"Microsoft.ForzaHorizon3BlizzardMountain", 0x2d30d30},
    {"Microsoft.ForzaHorizon3HotWheels", 0x2d30d80},
};
#define NKNOWN (sizeof known / sizeof known[0])

struct entry {
    const char *manifest;
    const char *tree;
    unsigned long long product_rva;
    const char *version;
};

static struct entry g_list[NKNOWN];
static char g_manifest[NKNOWN][72], g_tree[NKNOWN][72], g_version[NKNOWN][32];
static HANDLE g_log = INVALID_HANDLE_VALUE;

/* ---- the folder azimuth.exe is in ----------------------------------------------------------------------------- */

static void exe_dir(char *out, DWORD n)
{
    DWORD len = GetModuleFileNameA(NULL, out, n);
    if (len >= n)
        len = 0;
    while (len > 0 && out[len - 1] != '\\' && out[len - 1] != '/')
        len--;
    out[len] = 0;
}

static void say(const char *fmt, ...)
{
    char line[1024];
    va_list ap;
    va_start(ap, fmt);
    int n = vsnprintf(line, sizeof line - 2, fmt, ap);
    va_end(ap);
    if (n < 0)
        return;
    if (n > (int)sizeof line - 3)
        n = (int)sizeof line - 3;
    line[n++] = '\r';
    line[n++] = '\n';
    if (g_log == INVALID_HANDLE_VALUE) {
        char path[MAX_PATH * 2];
        exe_dir(path, MAX_PATH);
        strncat(path, "fh3-expansions.log", sizeof path - strlen(path) - 1);
        g_log = CreateFileA(path, GENERIC_WRITE, FILE_SHARE_READ, NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
        if (g_log == INVALID_HANDLE_VALUE)
            return;
    }
    DWORD w;
    WriteFile(g_log, line, (DWORD)n, &w, NULL);
}

/* ---- the package list -------------------------------------------------------------------------------------------- */

static int is_sha256_hex(const char *s)
{
    if (strlen(s) != 64)
        return 0;
    for (; *s; s++)
        if (!((*s >= '0' && *s <= '9') || (*s >= 'a' && *s <= 'f')))
            return 0;
    return 1;
}

/* Azimuth takes 1 to 23 characters of digits and dots (validator 0x1401a2396..0x1401a241b). */
static int is_version(const char *s)
{
    size_t n = strlen(s);
    if (n < 1 || n > 23)
        return 0;
    for (; *s; s++)
        if (!((*s >= '0' && *s <= '9') || *s == '.'))
            return 0;
    return 1;
}

/* Reads fh3-expansions.ini: one section per package identity with manifest=, tree= and version=. */
static int read_list(void)
{
    char ini[MAX_PATH * 2];
    exe_dir(ini, MAX_PATH);
    strncat(ini, "fh3-expansions.ini", sizeof ini - strlen(ini) - 1);
    if (GetFileAttributesA(ini) == INVALID_FILE_ATTRIBUTES) {
        say("no %s: run the expansions installer for this Azimuth folder", ini);
        return 0;
    }
    int n = 0;
    for (size_t i = 0; i < NKNOWN; i++) {
        char *m = g_manifest[n], *t = g_tree[n], *v = g_version[n];
        GetPrivateProfileStringA(known[i].identity, "manifest", "", m, sizeof g_manifest[0], ini);
        GetPrivateProfileStringA(known[i].identity, "tree", "", t, sizeof g_tree[0], ini);
        GetPrivateProfileStringA(known[i].identity, "version", "", v, sizeof g_version[0], ini);
        if (!m[0] && !t[0] && !v[0])
            continue; /* this package is not installed here */
        if (!is_sha256_hex(m) || !is_sha256_hex(t) || !is_version(v)) {
            say("%s: [%s] is unreadable (manifest, tree and version expected); left out", ini, known[i].identity);
            continue;
        }
        g_list[n].manifest = m;
        g_list[n].tree = t;
        g_list[n].product_rva = known[i].product_rva;
        g_list[n].version = v;
        say("package %s %s: manifest %s, tree %s", known[i].identity, v, m, t);
        n++;
    }
    return n;
}

/* ---- azimuth.exe ----------------------------------------------------------------------------------------------- */

/* Is Forza Horizon 3 added in this Azimuth (data\azimuth.ini, UTF-16, section [title-4ce1dfab] with a folder=)?
 * Without it there is nothing for FH3's list to do, and the DLL only forwards DirectInput. */
static int fh3_added(void)
{
    wchar_t ini[MAX_PATH * 2], folder[MAX_PATH];
    DWORD len = GetModuleFileNameW(NULL, ini, MAX_PATH);
    if (len == 0 || len >= MAX_PATH)
        return 0;
    while (len > 0 && ini[len - 1] != L'\\' && ini[len - 1] != L'/')
        len--;
    ini[len] = 0;
    wcscat(ini, L"data\\azimuth.ini");
    GetPrivateProfileStringW(L"title-4ce1dfab", L"folder", L"", folder, MAX_PATH, ini);
    return folder[0] != 0;
}

static void install(void)
{
    if (!fh3_added()) {
        say("Forza Horizon 3 is not added in this Azimuth; nothing changed (DirectInput is only forwarded)");
        return;
    }
    BYTE *base = (BYTE *)GetModuleHandleW(NULL);
    IMAGE_DOS_HEADER *dos = (IMAGE_DOS_HEADER *)base;
    if (!base || dos->e_magic != IMAGE_DOS_SIGNATURE) {
        say("the process image has no DOS header; nothing changed");
        return;
    }
    IMAGE_NT_HEADERS64 *nt = (IMAGE_NT_HEADERS64 *)(base + dos->e_lfanew);
    DWORD stamp = nt->FileHeader.TimeDateStamp, size = nt->OptionalHeader.SizeOfImage;
    if (stamp != AZ_TIMESTAMP) {
        say("this is not azimuth.exe 1.0.4 (PE timestamp 0x%08lx, want 0x%08x); nothing changed", stamp, AZ_TIMESTAMP);
        return;
    }
    if (size == AZ_SIZE_IMAGE_PATCHED) {
        say("azimuth.exe is the build patched by azimuth-dlc and carries its own package list; nothing changed");
        return;
    }
    if (size != AZ_SIZE_IMAGE) {
        say("azimuth.exe 1.0.4 with an unknown image size 0x%lx; nothing changed", size);
        return;
    }
    BYTE *rec = base + FH3_RECORD_RVA;
    if (*(ULONGLONG *)rec != (ULONGLONG)(base + FH3_TITLE_ID_RVA) ||
        memcmp(base + FH3_TITLE_ID_RVA, L"4ce1dfab", 16) != 0) {
        say("FH3's title record is not where 1.0.4 has it; nothing changed");
        return;
    }
    ULONGLONG *ptr = (ULONGLONG *)(rec + LIST_PTR_OFF), *count = (ULONGLONG *)(rec + LIST_COUNT_OFF);
    if (*ptr || *count) {
        say("FH3 already has a content-package list (%llu entries); nothing changed", (unsigned long long)*count);
        return;
    }
    int n = read_list();
    if (n == 0) {
        say("no package to add; nothing changed");
        return;
    }
    DWORD old;
    if (!VirtualProtect(ptr, 16, PAGE_READWRITE, &old)) {
        say("VirtualProtect failed (error %lu); nothing changed", GetLastError());
        return;
    }
    *count = (ULONGLONG)n;
    *ptr = (ULONGLONG)(ULONG_PTR)g_list;
    VirtualProtect(ptr, 16, old, &old);
    say("FH3's content-package list set: %d package(s); azimuth.exe on disk is unchanged", n);
}

/* ---- dinput8 exports ------------------------------------------------------------------------------------------
 * Forwarded to dinput8.next.dll next to azimuth.exe when there is one (another mod that also comes as dinput8.dll,
 * moved aside by the expansions setup, chain-loaded at process start as it would have been), otherwise to the
 * system's dinput8.dll. */

static HMODULE g_next;

/* A file that is another copy of this mod is never chained (it would load itself in a loop). */
static int is_this_mod(const wchar_t *path)
{
    HANDLE h = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE)
        return 0;
    static char buf[256 * 1024];
    DWORD got = 0;
    ReadFile(h, buf, sizeof buf, &got, NULL);
    CloseHandle(h);
    const char *needle = "fh3-expansions ";
    size_t nl = strlen(needle);
    for (DWORD i = 0; i + nl <= got; i++)
        if (memcmp(buf + i, needle, nl) == 0)
            return 1;
    return 0;
}

static void load_next(void)
{
    wchar_t path[MAX_PATH * 2];
    DWORD len = GetModuleFileNameW(NULL, path, MAX_PATH);
    if (len == 0 || len >= MAX_PATH)
        return;
    while (len > 0 && path[len - 1] != L'\\' && path[len - 1] != L'/')
        len--;
    path[len] = 0;
    wcscat(path, L"dinput8.next.dll");
    if (GetFileAttributesW(path) == INVALID_FILE_ATTRIBUTES)
        return;
    if (is_this_mod(path)) {
        say("dinput8.next.dll is another copy of this mod; not chained");
        return;
    }
    g_next = LoadLibraryW(path);
    if (g_next)
        say("chained dinput8.next.dll (another mod's dinput8.dll); DirectInput goes to it");
    else
        say("dinput8.next.dll did not load (error %lu); DirectInput goes to the system", GetLastError());
}

static HMODULE real_dinput8(void)
{
    static HMODULE h;
    if (!h && g_next && GetProcAddress(g_next, "DirectInput8Create"))
        h = g_next;
    if (!h) {
        wchar_t path[MAX_PATH + 16];
        UINT n = GetSystemDirectoryW(path, MAX_PATH);
        if (n == 0 || n >= MAX_PATH)
            return NULL;
        wcscat(path, L"\\dinput8.dll");
        h = LoadLibraryW(path);
    }
    return h;
}

static void *real(const char *name)
{
    HMODULE h = real_dinput8();
    return h ? (void *)GetProcAddress(h, name) : NULL;
}

typedef HRESULT(WINAPI *create_fn)(HINSTANCE, DWORD, REFIID, LPVOID *, LPUNKNOWN);
typedef HRESULT(WINAPI *void_fn)(void);
typedef HRESULT(WINAPI *class_fn)(REFCLSID, REFIID, LPVOID *);
typedef const void *(WINAPI *df_fn)(void);

HRESULT WINAPI proxy_DirectInput8Create(HINSTANCE inst, DWORD version, REFIID iid, LPVOID *out, LPUNKNOWN outer)
{
    static create_fn f;
    if (!f)
        f = (create_fn)real("DirectInput8Create");
    if (!f) {
        if (out)
            *out = NULL;
        return E_FAIL;
    }
    return f(inst, version, iid, out, outer);
}

HRESULT WINAPI proxy_DllCanUnloadNow(void)
{
    void_fn f = (void_fn)real("DllCanUnloadNow");
    return f ? f() : S_FALSE;
}

HRESULT WINAPI proxy_DllGetClassObject(REFCLSID clsid, REFIID iid, LPVOID *out)
{
    class_fn f = (class_fn)real("DllGetClassObject");
    if (!f) {
        if (out)
            *out = NULL;
        return CLASS_E_CLASSNOTAVAILABLE;
    }
    return f(clsid, iid, out);
}

HRESULT WINAPI proxy_DllRegisterServer(void)
{
    void_fn f = (void_fn)real("DllRegisterServer");
    return f ? f() : E_FAIL;
}

HRESULT WINAPI proxy_DllUnregisterServer(void)
{
    void_fn f = (void_fn)real("DllUnregisterServer");
    return f ? f() : E_FAIL;
}

const void *WINAPI proxy_GetdfDIJoystick(void)
{
    df_fn f = (df_fn)real("GetdfDIJoystick");
    return f ? f() : NULL;
}

BOOL WINAPI DllMain(HINSTANCE inst, DWORD reason, LPVOID reserved)
{
    (void)reserved;
    if (reason == DLL_PROCESS_ATTACH) {
        DisableThreadLibraryCalls(inst);
        say("fh3-expansions 1.1.0 (dinput8.dll)");
        install();
        load_next();
        if (g_log != INVALID_HANDLE_VALUE) {
            CloseHandle(g_log);
            g_log = INVALID_HANDLE_VALUE;
        }
    }
    return TRUE;
}
