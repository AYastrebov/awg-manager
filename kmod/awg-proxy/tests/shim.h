/* SPDX-License-Identifier: GPL-2.0 */
/*
 * Host-build shim for kernel module sources.
 *
 * Used by kmod/awg-proxy/tests to compile cps.c (and parts of
 * transform.c) on a regular dev machine with vanilla gcc — no Linux
 * kernel headers required. The shim provides just enough kernel types,
 * helpers, and stubs to satisfy the source files under test.
 *
 * Tests that need determinism (e.g. random-counter init) seed our PRNG
 * via shim_set_random_seed() and fix the clock via shim_set_fixed_time().
 */
#ifndef AWG_PROXY_TEST_SHIM_H
#define AWG_PROXY_TEST_SHIM_H

#define _DEFAULT_SOURCE  /* expose htobe32 / htole32 from <endian.h> */
#include <stdint.h>
#include <stddef.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <endian.h>

/* ---- Linux integer aliases ---- */
typedef uint8_t  u8;
typedef uint16_t u16;
typedef uint32_t u32;
typedef uint64_t u64;
typedef int8_t   s8;
typedef int16_t  s16;
typedef int32_t  s32;
typedef int64_t  s64;
typedef uint16_t __le16;
typedef uint16_t __be16;
typedef uint32_t __le32;
typedef uint32_t __be32;

/* ---- Endianness helpers ---- */
#define cpu_to_le32(x) ((__le32)htole32(x))
#define cpu_to_be32(x) ((__be32)htobe32(x))
#define le32_to_cpu(x) le32toh((uint32_t)(x))
#define be32_to_cpu(x) be32toh((uint32_t)(x))

/* ---- Memory helpers ---- */
static inline void *kmalloc(size_t n, int gfp) { (void)gfp; return malloc(n); }
static inline void *kzalloc(size_t n, int gfp) { (void)gfp; return calloc(1, n); }
static inline void  kfree(void *p) { free(p); }
#define GFP_KERNEL 0

/* ---- Random + time stubs (deterministic for tests) ---- */
void shim_set_random_seed(uint32_t seed);
void shim_set_fixed_time(uint32_t unix_seconds);
void get_random_bytes(void *buf, int n);
uint64_t ktime_get_real_seconds(void);

/* ---- Warning macros (no-op on host) ---- */
#define WARN_ON_ONCE(cond) ({ int _c = !!(cond); if (_c) fprintf(stderr, "WARN_ON_ONCE %s\n", #cond); _c; })
#define pr_warn(fmt, ...)  fprintf(stderr, "[pr_warn] " fmt, ##__VA_ARGS__)

#endif /* AWG_PROXY_TEST_SHIM_H */
