# 创建对象文件目录
mkdir -p obj

# 编译所有非测试的 cpp 文件
# 注意：-I. 表示在当前目录查找头文件
# -m64 确保生成 64 位代码 (SGX 需要)
# -O2 优化等级
CFLAGS="-c -O2 -m64 -DBLS12381 -DGFp_64 -I."

echo "Starting build with flags: $CFLAGS"

for file in *.cpp; do
  if [[ $file != test*.cpp ]]; then
    echo "Compiling $file..."
    g++ -c -O2 -m64 -I. $file -o obj/${file%.cpp}.o
  fi
done

# 打包成静态库
ar rcs libcore.a obj/*.o
