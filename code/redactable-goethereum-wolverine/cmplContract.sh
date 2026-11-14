make clean

make all

solc --abi --bin -o contractBuild solCode/PermissionManagerV2.sol --overwrite

./build/bin/abigen --bin=contractBuild/PermissionManagerV2.bin --abi=contractBuild/PermissionManagerV2.abi --pkg=contracts --out=contracts/PermissionManagerV2.go

make clean