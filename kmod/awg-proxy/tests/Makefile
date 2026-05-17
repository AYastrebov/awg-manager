# Host-only unit tests for kmod/awg-proxy/src/cps.c.
#
# Build/run on a regular dev machine — NO Linux kernel headers required.
# We provide minimal stubs (in stubs/) for the few <linux/*> and <asm/*>
# includes that cps.c pulls in, and shim.h supplies the integer types,
# byte-order macros, and deterministic get_random_bytes / ktime_*.
#
# Targets:
#   make           run tests (default)
#   make test      run tests
#   make clean     remove built artifacts
#
# To keep determinism, every test seeds the PRNG via shim_set_random_seed()
# and pins the clock via shim_set_fixed_time(). Byte-exact assertions are
# reproducible across machines / Go versions / compiler revisions.

CC      ?= gcc
CFLAGS  := -O0 -g3 -std=c11 -Wall -Wextra -Werror \
           -Wno-pointer-sign -Wno-unused-parameter \
           -I . -I stubs -include shim.h
LDFLAGS :=

TARGET  := test_cps
SOURCES := test_cps.c shim.c ../src/cps.c

.PHONY: all test clean

all: test

test: $(TARGET)
	./$(TARGET)

$(TARGET): $(SOURCES) shim.h
	$(CC) $(CFLAGS) -o $@ $(SOURCES) $(LDFLAGS)

clean:
	rm -f $(TARGET) *.o
