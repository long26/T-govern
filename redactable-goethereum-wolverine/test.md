# 测试锻造函数
1. 在测试文件中，我们已经实现了完整的变色龙哈希测试流程：
- 首先创建了原始消息 msg1 (值为123) 并计算其哈希值 hash1
- 然后创建了新消息 msg2 (值为456)
- 使用锻造函数生成新的随机数 newR
- 使用新消息和锻造后的随机数计算哈希值 hash2
- 最后验证 hash1 和 hash2 相等
2. 测试还包含了额外的验证：
- 验证了使用错误私钥无法成功锻造
- 测试了边界条件（零消息和大数消息）
- 验证了不同消息使用相同随机数会产生不同的原始哈希
```
chameleonhash_test.go:12: 系统参数: p = 18a2659b901904df6d22d5ee91c359967cd7d83317b7642b936b5fbe94860326b, q = c5132cdc80c826fb6916af748e1accb3e6bec198bdbb215c9b5afdf4a4301935, g = 10389c79e61ff45f1592eafafb23f690442f45abace732256d1ab13353a97997b
    chameleonhash_test.go:16: 公钥: 3ff418f425b5abd9baf9f8d54d213b6dd1daf5b1cf795081781b120b4dfab374
    chameleonhash_test.go:17: 私钥: 4c0d123f2480178c57e6c004b175f388bec0e070ff32691e635a64e784ff1d56
    chameleonhash_test.go:22: 消息1: 123, 随机数r: 531a62e22ad71643891aa50b20273df569ce236659133cfd7e8daf77d7459d7
    chameleonhash_test.go:24: 哈希值1: a6639205483227aac2b1c719dbf589833b097deac9824850880018f4a37b1188
    chameleonhash_test.go:28: 重复计算的哈希值1: a6639205483227aac2b1c719dbf589833b097deac9824850880018f4a37b1188
    chameleonhash_test.go:35: 消息2: 456
    chameleonhash_test.go:37: 锻造后的随机数r': a0d685ad4ddce5276e92693a881b0936ed3d55c25d8c6d15f47cdf47fe25f06f
    chameleonhash_test.go:39: 锻造后的哈希值2: a6639205483227aac2b1c719dbf589833b097deac9824850880018f4a37b1188
    chameleonhash_test.go:48: 错误的私钥: 9f4c5612f0293e7113051969aba8ea69150d72dbf3bf65ff95f477b5b53454a8
    chameleonhash_test.go:50: 使用错误私钥锻造的随机数r': 7890debc3485c08eaa2e7241505312191bf641790b9e73f35bdceedd0b772fe9
    chameleonhash_test.go:52: 使用错误私钥锻造的哈希值: af8c1d21508829e2bd5a4e9f5b9eb484d68a87881a17b03f1cd7ed9a3675d486
    chameleonhash_test.go:62: 零消息测试: msg = 0
    chameleonhash_test.go:64: 零消息的哈希值: b02c609e815733ecaae76c1fc4dd5824c5ee894b4be77529379a7614a6584d69
    chameleonhash_test.go:71: 大数消息测试: msg = 10000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000
    chameleonhash_test.go:73: 大数消息的哈希值: c7e2f60cc52b4ed481e589a6949914b3c369fd09b51946ee451fba97a59f6e45
    chameleonhash_test.go:80: 消息3: 789
    chameleonhash_test.go:82: 消息3的哈希值: 14abbc7da690a74b4494437c3af9c6db474edac5d97a0f5cb7cc5aa9781925d8e
PASS
```