# --------------- 用户可覆盖 -----------------
SGX_SDK  ?= /opt/intel/sgxsdk
SGX_MODE ?= SIM
SGX_ARCH ?= x64
# ------------------------------------------

# 引入 SDK 公共变量
include $(SGX_SDK)/buildenv.mk

# 工具链
SGX_EDGER8R := $(SGX_SDK)/bin/$(SGX_ARCH)/sgx_edger8r
SGX_SIGNER  := $(SGX_SDK)/bin/$(SGX_ARCH)/sgx_sign

SGX_LIBRARY_PATH := $(SGX_SDK)/lib64
SGX_MODE_STAMP := .sgx-build-mode

ifneq ($(SGX_MODE), HW)
    Urts_Library_Name := sgx_urts_sim
    Trts_Library_Name := sgx_trts_sim
    Service_Library_Name := sgx_tservice_sim
    Dcap_Link :=
else
    Urts_Library_Name := sgx_urts
    Trts_Library_Name := sgx_trts
    Service_Library_Name := sgx_tservice
    Dcap_Link := -lsgx_dcap_ql
endif

# 文件路径
EDL          := enclave/Enclave.edl
EDGE_U_C     := app/Enclave_u.c
EDGE_T_C     := enclave/Enclave_t.c
EDGE_U_OBJ   := app/Enclave_u.o
EDGE_T_OBJ   := enclave/Enclave_t.o

MIRACL_ROOT := lib/miracl
MIRACL_INC  := $(MIRACL_ROOT)
MIRACL_LIB  := $(MIRACL_ROOT)

SECP256K1_ROOT := ../crypto/secp256k1/libsecp256k1
SECP256K1_INC  := $(SECP256K1_ROOT)/include
SECP256K1_SRC  := $(SECP256K1_ROOT)/src
SECP256K1_OBJ  := secp256k1_sgx.o

# Enclave 源文件
ENCLAVE_SRCS := enclave/enclave.cpp \
                enclave/src/miracl_bls12381.cpp \
                enclave/src/setup.cpp \
                enclave/src/distkg.cpp \
                enclave/src/adapt.cpp \
                enclave/src/serialization.cpp
ENCLAVE_OBJS := $(ENCLAVE_SRCS:.cpp=.o)

# App 侧编译/链接标志
App_Cpp_Flags := -Iapp -I$(SGX_SDK)/include $(SGX_COMMON_CXXFLAGS) -fPIC
App_Link_Flags := -L$(SGX_LIBRARY_PATH) \
                  -L/usr/lib/x86_64-linux-gnu \
                  -Wl,-rpath,$(SGX_LIBRARY_PATH) \
                  -Wl,-rpath,/usr/lib/x86_64-linux-gnu \
                  $(Dcap_Link) \
                  -l$(Urts_Library_Name) \
                  -lpthread

# Enclave 侧编译/链接标志
Enclave_Config := enclave/Enclave.config.xml
Enclave_Key    := enclave/Enclave_private.pem
Enclave_C_Flags := -nostdinc -fvisibility=hidden -fpie \
				   -ffunction-sections -fdata-sections \
                   -I$(SGX_SDK)/include \
                   -I$(SGX_SDK)/include/tlibc \
                   -I$(SGX_SDK)/include/libcxx \
                   -I$(SGX_SDK)/include/sgx_trts \
                   -I. \
                   -Ienclave \
                   -Ienclave/nitch \
                   -Ienclave/src
Enclave_Link_Flags := \
        -L$(SGX_LIBRARY_PATH) \
        -Wl,--whole-archive -l$(Trts_Library_Name) -Wl,--no-whole-archive \
        -Wl,--start-group \
          -lsgx_tstdc -lsgx_tcxx -lsgx_tcrypto -l$(Service_Library_Name) -lsgx_dcap_tvl \
        -Wl,--end-group \
        -Wl,-z,relro,-z,now,-z,noexecstack \
        -nostdlib -nodefaultlibs -nostartfiles \
        -Wl,-Bstatic -Wl,-Bsymbolic -Wl,--no-undefined \
        -Wl,-pie,-eenclave_entry -Wl,--export-dynamic \
        -Wl,--defsym,__ImageBase=0 \
        -Wl,--version-script=enclave/Enclave.lds

Enclave_C_Flags   += -I/usr/include

Enclave_C_Flags += -I$(MIRACL_INC)

Enclave_C_Flags += -I$(SECP256K1_INC) -I$(SECP256K1_SRC) \
                    -I$(SECP256K1_ROOT) \
                    -DSECP256K1_BUILD=1 \
                    -DSECP256K1_ENABLE_MODULE_RECOVERY=1 \
                    -DENABLE_MODULE_RECOVERY=1

Enclave_Libs := -L$(MIRACL_LIB) -lcore

# 默认目标
.PHONY: all clean prepare-sgx-mode sgx-artifacts
all: prepare-sgx-mode
	@$(MAKE) --no-print-directory sgx-artifacts

sgx-artifacts: libsgx_module.so enclave.signed.so

# SGX_MODE changes the trusted and untrusted runtime libraries but make does not
# otherwise see it as a file dependency. Remove stale cross-mode objects before
# evaluating the real targets so `make ... SGX_MODE=SIM/HW` is reliable.
prepare-sgx-mode:
	@if [ "$$(cat $(SGX_MODE_STAMP) 2>/dev/null || true)" != "$(SGX_MODE)" ]; then \
		echo "SGX mode changed; rebuilding artifacts for $(SGX_MODE)"; \
		$(MAKE) --no-print-directory clean; \
		printf '%s\n' "$(SGX_MODE)" > $(SGX_MODE_STAMP); \
	fi

# 1. 生成 Untrusted Edge 文件
# Table 6.2 REE baseline. It is a normal host executable that copies the
# enclave's algorithmic code paths (NITCH/MIRACL + libsecp256k1) without SGX
# lifecycle or ECALL cost. It is the comparison baseline for the enclave
# instrumentation below.
TABLE_6_2_REE_BENCH := enclave/bench/table_6_2_ree_bench
TABLE_6_2_REE_SECP_OBJ := enclave/bench/secp256k1_ree.o
TABLE_6_2_REE_BENCH_SRCS := enclave/bench/table_6_2_ree_bench.cpp \
                            enclave/src/miracl_bls12381.cpp \
                            enclave/src/distkg.cpp \
                            enclave/src/serialization.cpp
TABLE_6_2_REE_BENCH_FLAGS := -std=c++17 -O3 -Ienclave/bench -I. -Ienclave -Ienclave/nitch -I$(MIRACL_INC) \
                             -I$(SECP256K1_INC) -I$(SECP256K1_SRC) -I$(SECP256K1_ROOT)
TABLE_6_2_REE_SECP_FLAGS := -O3 -I$(SECP256K1_INC) -I$(SECP256K1_SRC) -I$(SECP256K1_ROOT) \
                            -DSECP256K1_BUILD=1 -DSECP256K1_ENABLE_MODULE_RECOVERY=1 \
                            -DENABLE_MODULE_RECOVERY=1 -DUSE_BASIC_CONFIG=1 \
                            -include $(SECP256K1_SRC)/basic-config.h

.PHONY: table-6-2-ree-bench test-table-6-2-ree-bench run-table-6-2-ree-bench
table-6-2-ree-bench: $(TABLE_6_2_REE_BENCH)

$(TABLE_6_2_REE_SECP_OBJ): $(SECP256K1_SRC)/secp256k1.c $(SECP256K1_SRC)/basic-config.h
	@mkdir -p $(dir $@)
	$(CC) $(TABLE_6_2_REE_SECP_FLAGS) -c $< -o $@

$(TABLE_6_2_REE_BENCH): $(TABLE_6_2_REE_BENCH_SRCS) $(TABLE_6_2_REE_SECP_OBJ) enclave/nitch/*.hpp $(MIRACL_LIB)/libcore.a
	@mkdir -p $(dir $@)
	$(CXX) $(TABLE_6_2_REE_BENCH_FLAGS) $(TABLE_6_2_REE_BENCH_SRCS) $(TABLE_6_2_REE_SECP_OBJ) -L$(MIRACL_LIB) -lcore -o $@

test-table-6-2-ree-bench: $(TABLE_6_2_REE_BENCH)
	$(TABLE_6_2_REE_BENCH) --self-test

run-table-6-2-ree-bench: $(TABLE_6_2_REE_BENCH)
	$(TABLE_6_2_REE_BENCH)

# Standalone committee authorization-certificate generator for Table 6.2 op 1
# (enclave verify_cert_req_auth). Signs with the exact MIRACL BLS the enclave
# verifies with, so its output is accepted as-is. Independent of the REE
# baseline binary.
COMMITTEE_CERT_HELPER := enclave/bench/committee_cert_helper
.PHONY: committee-cert-helper
committee-cert-helper: $(COMMITTEE_CERT_HELPER)

$(COMMITTEE_CERT_HELPER): enclave/bench/committee_cert_helper.cpp $(MIRACL_LIB)/libcore.a
	$(CXX) -std=c++17 -O2 -I. -I$(MIRACL_INC) $< -L$(MIRACL_LIB) -lcore -o $@

$(EDGE_U_C): $(EDL)
	@echo "GEN  =>  $@"
	@cd app && $(SGX_EDGER8R) --untrusted $(realpath $<) \
	                          --search-path $(realpath enclave) \
	                          --search-path $(SGX_SDK)/include

# 2. 生成 Trusted Edge 文件
$(EDGE_T_C): $(EDL)
	@echo "GEN  =>  $@"
	@cd enclave && $(SGX_EDGER8R) --trusted $(realpath $<) \
	                              --search-path $(realpath .) \
	                              --search-path $(SGX_SDK)/include

# 3. 编译 Edge 对象
$(EDGE_U_OBJ): $(EDGE_U_C)
	$(CC) $(SGX_COMMON_CFLAGS) $(App_Cpp_Flags) -c $< -o $@

$(EDGE_T_OBJ): $(EDGE_T_C)
	$(CC) $(SGX_COMMON_CFLAGS) $(Enclave_C_Flags) -c $< -o $@

# 4. App 共享库（供 Go 链接）
libsgx_module.so: app/tee.cpp app/ErrorSupport.cpp $(EDGE_U_OBJ)
	$(CXX) -shared -fPIC $^ -o $@ $(App_Cpp_Flags) $(App_Link_Flags)
	@echo "LINK =>  $@"

# 5. 编译 Enclave 各源文件
enclave/enclave.o: enclave/enclave.cpp $(EDGE_T_C) enclave/nitch/*.hpp
	$(CXX) $(Enclave_C_Flags) -c $< -o $@

enclave/src/miracl_bls12381.o: enclave/src/miracl_bls12381.cpp enclave/nitch/miracl_bls12381.hpp enclave/nitch/types.hpp
	$(CXX) $(Enclave_C_Flags) -c $< -o $@

enclave/src/setup.o: enclave/src/setup.cpp enclave/nitch/setup.hpp enclave/nitch/miracl_bls12381.hpp
	$(CXX) $(Enclave_C_Flags) -c $< -o $@

enclave/src/distkg.o: enclave/src/distkg.cpp enclave/nitch/distkg.hpp enclave/nitch/miracl_bls12381.hpp
	$(CXX) $(Enclave_C_Flags) -c $< -o $@

enclave/src/adapt.o: enclave/src/adapt.cpp enclave/nitch/adapt.hpp enclave/nitch/miracl_bls12381.hpp
	$(CXX) $(Enclave_C_Flags) -c $< -o $@

enclave/src/serialization.o: enclave/src/serialization.cpp enclave/nitch/serialization.hpp
	$(CXX) $(Enclave_C_Flags) -c $< -o $@

secp256k1_sgx.o: $(SECP256K1_SRC)/secp256k1.c enclave/secp256k1_sgx_compat.h
	$(CC) $(SGX_COMMON_CFLAGS) $(Enclave_C_Flags) \
        -include enclave/secp256k1_sgx_compat.h \
        -include $(SECP256K1_SRC)/basic-config.h \
        -DUSE_BASIC_CONFIG=1 \
        -c $< -o $@

# 6. 链接 Enclave 本体
enclave.so: $(ENCLAVE_OBJS) $(EDGE_T_OBJ) $(SECP256K1_OBJ)
	$(CXX) -L$(SGX_LIBRARY_PATH) \
        -Wl,--whole-archive -l$(Trts_Library_Name) -Wl,--no-whole-archive \
        -nostdlib -nodefaultlibs -nostartfiles \
        -Wl,-z,relro,-z,now,-z,noexecstack \
        -Wl,-Bstatic -Wl,-Bsymbolic -Wl,--no-undefined \
        -Wl,-pie,-eenclave_entry -Wl,--export-dynamic \
        -Wl,--defsym,__ImageBase=0 \
        -Wl,--version-script=enclave/Enclave.lds \
        -Wl,--start-group $^ -lsgx_tstdc -lsgx_tcxx -lsgx_tcrypto -l$(Service_Library_Name) -lsgx_dcap_tvl -lcore -L$(MIRACL_LIB) -Wl,--end-group \
        -o $@
	@echo "LINK =>  enclave.so"

# 7. 签名
enclave.signed.so: enclave.so $(Enclave_Config)
	@[ -f $(Enclave_Key) ] || openssl genrsa -out $(Enclave_Key) -3 3072
	$(SGX_SIGNER) sign -key $(Enclave_Key) -enclave $< -out $@ -config $(Enclave_Config)
	@echo "SIGN =>  $@"

# 8. 清理
clean:
	rm -f *.so app/Enclave_u.* enclave/Enclave_t.* enclave.so enclave.signed.so
	rm -f $(ENCLAVE_OBJS) $(SECP256K1_OBJ)
	rm -f $(TABLE_6_2_REE_BENCH) $(TABLE_6_2_REE_SECP_OBJ) $(COMMITTEE_CERT_HELPER)
	rm -f $(SGX_MODE_STAMP)
