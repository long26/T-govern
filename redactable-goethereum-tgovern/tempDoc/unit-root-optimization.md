# 单位根优化整理

这份笔记整理的是当前分支从 `feat-引入单位根` 开始到 `feat-resharing` 的实现演进，目标是把 Go 版里“把插值域切到单位根域，再用 FFT 加速 Lagrange 计算”的思路，抽象成后续 C++ 版可以直接复用的设计说明。

## 提交演进

| commit | 作用 | 结论 |
| --- | --- | --- |
| `24e44aa` | `feat-引入单位根` | 把 DistKG / Combine 的评估点从 `1..n` 改成 `x_j = ω^j`，并加入 `n` 为 2 的幂的约束。 |
| `680f646` | `feat-FFT` | 把 Combine 里逐项计算 Lagrange 系数的方式，替换成“根多项式 + FFT + 求导 + 批量求逆”的路线。 |
| `ad8f382` | `fix-verify hash截断` | Verify 不再把 GT 输出截断成 32 字节，而是比较完整序列化结果。这个不是单位根优化本身，但会影响联调。 |
| `b6053ab` | `fix-numParties传入` | 调用链修正为把真实委员会规模 `committeeSize` 传进 `Combine`，保证域大小和实际参与方一致。 |
| `0ec3140` | `feat-resharing` | 把同一套单位根域 + FFT 插值逻辑复用到 DKRS / resharing 流程，说明它已经变成通用基础设施。 |

## 核心思路

原实现的思路是把份额放在整数点上：`f(j)`，然后在 Combine 里对每个选中的索引做一次完整的拉格朗日乘积，最后逐个合并系数。这样写法简单，但本质上是二次复杂度，随着委员会变大，系数计算会很重。

优化后的做法有两层：

1. 先把评估域换成单位根域 `x_j = ω^j`，其中 `ω` 是 `F_p` 上的 primitive `n`-th root of unity。
2. 再把 Lagrange 系数计算改成根多项式路线：
   - 构造 `P(x) = ∏ (x - x_i)`
   - 计算 `P'(x)`
   - 直接用 `λ_i(0) = -P(0) / (x_i · P'(x_i))`
   - 对所有 `x_i · P'(x_i)` 做 batch inverse，避免重复求逆

这条路线的关键收益不是“公式更漂亮”，而是把原来重复的逐项相乘，替换成可以用 FFT 复用的多项式乘法与求值。对较大的委员会来说，收益会非常明显。

## 代码层面的落点

- `SimulatedDistKG` 和 `CreateDistKGDeal` 不再用 `j` 作为评估点，而是用 `evaluationPointForIndex(pp.p, n, j)` 先算出 `ω^j`，再做多项式求值。
- `VerifyDistKGShare` / `VerifyDKRSShare` 同样从 `receiverIndex` 反推出 `x_j = ω^j`，验证公式也从 `j^k` 切成 `(ω^j)^k`。
- `Combine` 里不再对每个份额单独做普通 Lagrange 乘积，而是先把选中的索引映射到单位根域，再走 `lagrangeCoefficientsAtZeroGnarkFFT`。
- `DKRSExtract` 也复用同样的域映射和系数计算，这说明“单位根域”不是某一个函数的局部技巧，而是整个协议层的共同前提。

## 迁移到 C++ 时最容易踩的坑

- `n` 不能随便选，当前实现要求它是 2 的幂；同时它还必须满足 `n | (p - 1)`，否则根本找不到对应的 primitive root of unity。
- `Combine` 不能只看本地收集到的 shares，必须知道真实的委员会规模 `numParties / committeeSize`，否则根域点没法正确重建。`b6053ab` 修的就是这个问题。
- 成员索引是 1-based，映射公式是 `x_j = ω^j`，不要把 `j` 当成 0-based 下标直接塞进公式里。
- 选进 Combine 的 shares 必须先做去重和范围校验；重复索引会让分母变成 0，越界索引会让域点非法。
- 如果以后 C++ 版支持多个模数，root cache 不能只按 `n` 缓存，应该按 `(modulus, n)` 缓存。Go 版当前默认只面对 BLS12-381 scalar field，所以缓存粒度比较粗。
- root / twiddle 这类缓存对象最好只读返回，不要把同一个可变对象直接暴露给上层。Go 版这里是返回拷贝，C++ 版也应该保持这个习惯。
- `batch inverse` 和 `sum(λ_i(0)) == 1` 这两个校验很适合保留成断言或测试，能快速发现系数算错、点集重复、或域映射错位。
- `ad8f382` 提醒了一件容易被忽略的事：Verify 时不要把 GT 输出截断成 32 字节再比对，应该比较完整序列化结果。这个和单位根无关，但会直接影响最终验证是否成功。

## C++ 版建议

- 把“域配置”单独封装成一个对象，里面至少要保存 `p`、`n`、`ω`、以及 FFT 需要的 twiddle 数据。
- 把“索引到域点”的函数做成单独入口，所有 DistKG / Combine / resharing 路径统一调用，避免某条路径还在用旧的 `j`。
- 如果 FFT 库只支持 power-of-two 域，就保持当前约束；如果要放宽到其他 `n`，就要同步重写根选择、FFT 域创建和测试用例。
- 对小规模委员会，可以保留一个朴素实现作为对照测试，但主路径应该走 FFT 版本。

## 一句话总结

这次优化的本质是：把“整数点上的逐项插值”改造成“单位根域上的多项式运算”，再用 FFT 和 batch inverse 把 Combine / DistKG / DKRS 里重复的插值成本压下去。