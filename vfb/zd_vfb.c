// zd_vfb: run a CardputerZero app against a virtual screen and keyboard.
//
// Loaded with LD_PRELOAD by ZeroDesk. The app believes it is drawing to the
// LCD and reading the built-in keyboard, but:
//   /dev/fb*                      -> a shared-memory file (ZD_VFB)
//   the keyboard evdev node       -> a FIFO fed by ZeroDesk (ZD_VKBD)
//   any other /dev/input/event*   -> /dev/null (so it never sees real keys)
// The device's own screen and foreground app are left untouched.
//
// Build on the device: gcc -O2 -shared -fPIC -o zd_vfb.so zd_vfb.c -ldl

#define _GNU_SOURCE
#include <dirent.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <linux/fb.h>
#include <linux/input.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <unistd.h>

#define MAX_FD 4096

enum { KIND_NONE = 0, KIND_FB = 1, KIND_KBD = 2, KIND_NULLINPUT = 3 };

static unsigned char fd_kind[MAX_FD];
static pthread_once_t init_once = PTHREAD_ONCE_INIT;

static int (*real_open)(const char *, int, ...);
static int (*real_openat)(int, const char *, int, ...);
static int (*real_ioctl)(int, unsigned long, ...);
static int (*real_close)(int);

static const char *vfb_path;
static const char *vkbd_path;
static int width = 320, height = 170, stride = 640;
static char kbd_node[64];  // e.g. /dev/input/event3
static char kbd_name[128] = "tca8418c";

static void init(void) {
    real_open = dlsym(RTLD_NEXT, "open");
    real_openat = dlsym(RTLD_NEXT, "openat");
    real_ioctl = dlsym(RTLD_NEXT, "ioctl");
    real_close = dlsym(RTLD_NEXT, "close");
    vfb_path = getenv("ZD_VFB");
    vkbd_path = getenv("ZD_VKBD");
    if (getenv("ZD_W")) width = atoi(getenv("ZD_W"));
    if (getenv("ZD_H")) height = atoi(getenv("ZD_H"));
    if (getenv("ZD_STRIDE")) stride = atoi(getenv("ZD_STRIDE"));

    // find the built-in keyboard's event node by name
    DIR *d = opendir("/sys/class/input");
    if (d) {
        struct dirent *e;
        while ((e = readdir(d))) {
            if (strncmp(e->d_name, "event", 5) != 0) continue;
            char p[256], name[128] = {0};
            snprintf(p, sizeof p, "/sys/class/input/%s/device/name", e->d_name);
            FILE *f = fopen(p, "r");
            if (!f) continue;
            if (fgets(name, sizeof name, f) && strstr(name, "tca8418")) {
                name[strcspn(name, "\n")] = 0;
                snprintf(kbd_node, sizeof kbd_node, "/dev/input/%s", e->d_name);
                snprintf(kbd_name, sizeof kbd_name, "%s", name);
            }
            fclose(f);
        }
        closedir(d);
    }
}

static void ensure_init(void) { pthread_once(&init_once, init); }

static void mark(int fd, int kind) {
    if (fd >= 0 && fd < MAX_FD) fd_kind[fd] = (unsigned char)kind;
}

static int kind_of(int fd) { return (fd >= 0 && fd < MAX_FD) ? fd_kind[fd] : KIND_NONE; }

// Resolve symlinks such as /dev/input/by-path/...-event or /dev/fb_lcd.
static void canon(const char *path, char *out, size_t n) {
    if (!realpath(path, out)) snprintf(out, n, "%s", path);
}

static int classify(const char *path) {
    if (!path || strncmp(path, "/dev/", 5) != 0) return KIND_NONE;
    char c[PATH_MAX];
    canon(path, c, sizeof c);
    if (strncmp(c, "/dev/fb", 7) == 0) return KIND_FB;
    if (strncmp(c, "/dev/input/event", 16) == 0) {
        if (kbd_node[0] && strcmp(c, kbd_node) == 0) return KIND_KBD;
        return KIND_NULLINPUT;
    }
    return KIND_NONE;
}

static int redirect(int kind, int flags) {
    switch (kind) {
    case KIND_FB:
        if (!vfb_path) return -2;
        return real_open(vfb_path, O_RDWR | O_CLOEXEC);
    case KIND_KBD:
        if (!vkbd_path) return -2;
        // the FIFO is opened read/write so it never reports EOF when
        // ZeroDesk reconnects; keep the app's non-blocking choice
        return real_open(vkbd_path, O_RDWR | O_CLOEXEC | (flags & O_NONBLOCK));
    case KIND_NULLINPUT:
        return real_open("/dev/null", O_RDONLY | O_CLOEXEC);
    }
    return -2;
}

static int do_open(int dirfd, const char *path, int flags, mode_t mode, int at) {
    ensure_init();
    if (vfb_path) {
        int kind = classify(path);
        if (kind != KIND_NONE) {
            int fd = redirect(kind, flags);
            if (fd != -2) {
                mark(fd, kind);
                return fd;
            }
        }
    }
    int fd = at ? real_openat(dirfd, path, flags, mode) : real_open(path, flags, mode);
    mark(fd, KIND_NONE);
    return fd;
}

int open(const char *path, int flags, ...) {
    mode_t mode = 0;
    if (flags & (O_CREAT | O_TMPFILE)) {
        va_list ap;
        va_start(ap, flags);
        mode = va_arg(ap, mode_t);
        va_end(ap);
    }
    return do_open(AT_FDCWD, path, flags, mode, 0);
}
int open64(const char *path, int flags, ...) __attribute__((alias("open")));
int __open_2(const char *path, int flags) { return do_open(AT_FDCWD, path, flags, 0, 0); }
int __open64_2(const char *path, int flags) { return do_open(AT_FDCWD, path, flags, 0, 0); }

int openat(int dirfd, const char *path, int flags, ...) {
    mode_t mode = 0;
    if (flags & (O_CREAT | O_TMPFILE)) {
        va_list ap;
        va_start(ap, flags);
        mode = va_arg(ap, mode_t);
        va_end(ap);
    }
    return do_open(dirfd, path, flags, mode, 1);
}
int openat64(int dirfd, const char *path, int flags, ...) __attribute__((alias("openat")));
int __openat_2(int dirfd, const char *path, int flags) { return do_open(dirfd, path, flags, 0, 1); }
int __openat64_2(int dirfd, const char *path, int flags) { return do_open(dirfd, path, flags, 0, 1); }

int close(int fd) {
    ensure_init();
    mark(fd, KIND_NONE);
    return real_close(fd);
}

static void set_bits(void *buf, size_t len) { memset(buf, 0xff, len); }

static int fb_ioctl(unsigned long req, void *arg) {
    switch (req) {
    case FBIOGET_VSCREENINFO: {
        struct fb_var_screeninfo *v = arg;
        memset(v, 0, sizeof *v);
        v->xres = v->xres_virtual = (unsigned)width;
        v->yres = v->yres_virtual = (unsigned)height;
        v->bits_per_pixel = 16;
        v->red = (struct fb_bitfield){11, 5, 0};
        v->green = (struct fb_bitfield){5, 6, 0};
        v->blue = (struct fb_bitfield){0, 5, 0};
        v->activate = FB_ACTIVATE_NOW;
        v->height = v->width = (unsigned)-1;
        return 0;
    }
    case FBIOGET_FSCREENINFO: {
        struct fb_fix_screeninfo *f = arg;
        memset(f, 0, sizeof *f);
        snprintf(f->id, sizeof f->id, "zd-vfb");
        f->smem_len = (unsigned)(stride * height);
        f->type = FB_TYPE_PACKED_PIXELS;
        f->visual = FB_VISUAL_TRUECOLOR;
        f->line_length = (unsigned)stride;
        return 0;
    }
    case FBIOPUT_VSCREENINFO:
    case FBIOPAN_DISPLAY:
    case FBIOBLANK:
        return 0;
    default:
        // FBIO_WAITFORVSYNC and friends: pretend a ~60 Hz vsync
        if (_IOC_TYPE(req) == 'F') {
            usleep(16000);
            return 0;
        }
        errno = ENOTTY;
        return -1;
    }
}

static int kbd_ioctl(unsigned long req, void *arg) {
    unsigned type = _IOC_TYPE(req), nr = _IOC_NR(req), size = _IOC_SIZE(req);
    if (type != 'E') {
        errno = ENOTTY;
        return -1;
    }
    if (req == EVIOCGVERSION) {
        *(int *)arg = EV_VERSION;
        return 0;
    }
    if (req == EVIOCGID) {
        memset(arg, 0, sizeof(struct input_id));
        return 0;
    }
    if (req == EVIOCGRAB || req == EVIOCSCLOCKID) return 0;
    if (nr == _IOC_NR(EVIOCGNAME(0))) {
        snprintf(arg, size, "%s", kbd_name);
        return (int)strlen(kbd_name) + 1;
    }
    if (nr == _IOC_NR(EVIOCGPHYS(0)) || nr == _IOC_NR(EVIOCGUNIQ(0))) {
        if (size) ((char *)arg)[0] = 0;
        return 0;
    }
    if (nr >= _IOC_NR(EVIOCGBIT(0, 0)) && nr < _IOC_NR(EVIOCGBIT(EV_MAX, 0))) {
        // report key (and syn) capabilities with every key code set
        unsigned ev = nr - _IOC_NR(EVIOCGBIT(0, 0));
        memset(arg, 0, size);
        if (ev == 0) {
            ((unsigned char *)arg)[0] = (1 << EV_SYN) | (1 << EV_KEY);
        } else if (ev == EV_KEY) {
            set_bits(arg, size);
        }
        return (int)size;
    }
    if (nr == _IOC_NR(EVIOCGKEY(0)) || nr == _IOC_NR(EVIOCGLED(0)) || nr == _IOC_NR(EVIOCGSW(0))) {
        memset(arg, 0, size);
        return (int)size;
    }
    return 0;
}

int ioctl(int fd, unsigned long req, ...) {
    ensure_init();
    va_list ap;
    va_start(ap, req);
    void *arg = va_arg(ap, void *);
    va_end(ap);
    switch (kind_of(fd)) {
    case KIND_FB:
        return fb_ioctl(req, arg);
    case KIND_KBD:
        return kbd_ioctl(req, arg);
    case KIND_NULLINPUT:
        errno = ENOTTY;
        return -1;
    }
    return real_ioctl(fd, req, arg);
}
