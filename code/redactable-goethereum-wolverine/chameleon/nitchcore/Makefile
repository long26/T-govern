# nitchcore: vendored BLS12-381 crypto core (nitch layer + MIRACL core subset).
#
# Builds libnitchcore.a from src/*.cpp plus the vendored MIRACL subset.
#
# The miracl source set is FIXED at vendor time (see README.md for the subset
# list and the regeneration script). $(wildcard miracl/*.cpp) is used purely as
# a convenience; miracl/templates/ is deliberately outside the wildcard and is
# never compiled (upstream XXX/YYY/ZZZ templates + the generator).

CXX      ?= g++
AR       ?= ar
CXXFLAGS ?= -std=c++17 -O2 -fPIC -Wall
INCLUDES  = -I. -Initch -Imiracl -Isrc

NITCH_SRCS  = $(wildcard src/*.cpp) c_api.cpp
MIRACL_SRCS = $(wildcard miracl/*.cpp)
SRCS        = $(NITCH_SRCS) $(MIRACL_SRCS)
OBJS        = $(SRCS:.cpp=.o)

LIB  = libnitchcore.a
ORACLE = tools/h1_oracle
CC       ?= gcc
SELFTEST = tools/c_api_selftest

.PHONY: all clean smoke selftest

all: $(LIB)

$(LIB): $(OBJS)
	$(AR) rcs $@ $^

%.o: %.cpp
	$(CXX) $(CXXFLAGS) $(INCLUDES) -c $< -o $@

c_api.o: c_api.cpp c_api.h
	$(CXX) $(CXXFLAGS) $(INCLUDES) -c $< -o $@

# Link order matters: the object/oracle first, then the archive.
smoke: $(ORACLE)

$(ORACLE): tools/h1_oracle.cpp $(LIB)
	$(CXX) $(CXXFLAGS) -I. -Initch -Imiracl tools/h1_oracle.cpp $(LIB) -o $(ORACLE)

# Regression guard (docs/nitchcore-c-api-contract.md §6): the driver is
# compiled as C to prove c_api.h is a valid C header, then linked against the
# C++ archive. Runs the full c_api surface end-to-end in a single thread.
selftest: $(LIB) $(SELFTEST)
	./$(SELFTEST)

$(SELFTEST): tools/c_api_selftest.cpp c_api.h $(LIB)
	@mkdir -p tools
	$(CC) -std=c11 -O2 -Wall -Wextra -I. -x c -c tools/c_api_selftest.cpp -o tools/c_api_selftest.o
	$(CXX) $(CXXFLAGS) tools/c_api_selftest.o $(LIB) -o $(SELFTEST)

clean:
	rm -f $(OBJS) $(LIB) $(ORACLE) $(SELFTEST) tools/c_api_selftest.o
