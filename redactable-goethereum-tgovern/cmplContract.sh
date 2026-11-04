make clean

make all

solc --abi --bin -o contractBuild solCode/PermissionManagerV3.sol --overwrite

./build/bin/abigen --bin=contractBuild/PermissionManagerV3.bin --abi=contractBuild/PermissionManagerV3.abi --pkg=contracts --out=contracts/PermissionManagerV3.go

make clean
