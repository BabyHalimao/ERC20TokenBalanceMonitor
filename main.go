package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const MIN_ERC20_ABI_JSON = `[{"constant":true,"inputs":[{"name":"_owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"balance","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"}]`

var (
	ethCli    *ethclient.Client
	_erc20ABI abi.ABI
)

var (
	node      = flag.String("node", "https://rpc.merlinchain.io/", "")
	token     = flag.String("token", "0x967aEC3276b63c5E2262da9641DB9dbeBB07dC0d", "")
	addr      = flag.String("addr", "0x25aB3Efd52e6470681CE037cD546Dc60726948D3", "")
	addrAlias = flag.String("addrName", "Meson", "")
	threshold = flag.Float64("threshold", 2000, "")
	interval  = flag.String("interval", "3s", "")
	dingToken = flag.String("dingToken", "", "")
	muteDing  = flag.Bool("mute", false, "")
)

func main() {
	flag.Parse()
	var err error
	// check flag value
	if !*muteDing && *dingToken == "" {
		panic("dingToken shouldn't be empty when mute isn't true")
	}
	ethCli, err = ethclient.Dial(*node)
	if err != nil {
		panic(err)
	}
	_erc20ABI, err = abi.JSON(strings.NewReader(MIN_ERC20_ABI_JSON))
	if err != nil {
		panic(err)
	}
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGABRT,
		syscall.SIGKILL,
	)
	defer stop()

	tokenAddr := common.HexToAddress(*token)
	accAddr := common.HexToAddress(*addr)

	tokenSymbol, err := getERC20TokenSymbol(ctx, ethCli, tokenAddr)
	if err != nil {
		panic(err)
	}

	tokenDecimal, err := getERC20TokenDecimals(ctx, ethCli, tokenAddr)
	if err != nil {
		panic(err)
	}
	slog.With("decimals", tokenDecimal, "symbol", tokenSymbol, "tokenAddr", tokenAddr.Hex(), "accAddr", accAddr.Hex(), "accAlias", *addrAlias).Info("start monitor erc20 balance info")

	intervalDur, err := time.ParseDuration(*interval)
	if err != nil {
		panic(err)
	}
	ticker := time.NewTicker(intervalDur)
	for {
		select {
		case <-ctx.Done():
			slog.Info("process exit")
			return
		case <-ticker.C:
			bal, err := getERC20TokenBalance(context.Background(), ethCli, tokenAddr, accAddr)
			if err != nil {
				continue
			}
			balStr, balF := formatTokenAmtToHumanReadable(bal, int(tokenDecimal), 2)
			slog.With("b", balStr+tokenSymbol).Info("bal info")
			if balF >= *threshold && !*muteDing {
				dingDingNotify(*dingToken, fmt.Sprintf("%s %s balance %v >= %v", *addrAlias, tokenSymbol, balStr, *threshold), true, nil)
			}
		}
	}
}

func getERC20TokenDecimals(ctx context.Context, ethCli *ethclient.Client, token common.Address) (uint8, error) {
	data, err := _erc20ABI.Pack("decimals")
	if err != nil {
		slog.With("err", err).Error("getERC20TokenDecimals pack error")
		return 0, err
	}
	callMsg := ethereum.CallMsg{To: &token, Data: data}
	result, err := ethCli.CallContract(ctx, callMsg, nil)
	if err != nil {
		slog.With("err", err).Error("getERC20TokenDecimals error")
		return 0, err
	}
	valueAry, err := _erc20ABI.Unpack("decimals", result)
	if err != nil {
		slog.With("err", err).Error("getERC20TokenDecimals unpack error")
		return 0, err
	}
	return valueAry[0].(uint8), nil
}

func getERC20TokenSymbol(ctx context.Context, ethCli *ethclient.Client, token common.Address) (string, error) {
	data, err := _erc20ABI.Pack("symbol")
	if err != nil {
		slog.With("err", err).Error("getERC20TokenSymbol pack error")
		return "", err
	}
	callMsg := ethereum.CallMsg{To: &token, Data: data}
	result, err := ethCli.CallContract(ctx, callMsg, nil)
	if err != nil {
		slog.With("err", err).Error("getERC20TokenSymbol error")
		return "", err
	}
	valueAry, err := _erc20ABI.Unpack("symbol", result)
	if err != nil {
		slog.With("err", err).Error("getERC20TokenSymbol unpack error")
		return "", err
	}
	return valueAry[0].(string), nil
}

func getERC20TokenBalance(ctx context.Context, ethCli *ethclient.Client, token common.Address, acc common.Address) (*big.Int, error) {
	data, err := _erc20ABI.Pack("balanceOf", acc)
	if err != nil {
		slog.With("err", err).Error("getERC20TokenBalance pack error")
		return nil, err
	}
	callMsg := ethereum.CallMsg{To: &token, Data: data}
	result, err := ethCli.CallContract(ctx, callMsg, nil)
	if err != nil {
		slog.With("err", err).Error("getERC20TokenBalance error")
		return nil, err
	}
	valueAry, err := _erc20ABI.Unpack("balanceOf", result)
	if err != nil {
		slog.With("err", err).Error("getERC20TokenBalance unpack error")
		return nil, err
	}
	return valueAry[0].(*big.Int), nil
}

func formatTokenAmtToHumanReadable(amt *big.Int, decimal int, prec int) (string, float64) {
	amtBigFloat := new(big.Float).Quo(new(big.Float).SetInt(amt), big.NewFloat(math.Pow10(decimal)))
	amtStr := amtBigFloat.Text('f', prec)
	amtF, _ := strconv.ParseFloat(amtStr, 64)
	return amtStr, amtF
}

func dingDingNotify(token, content string, isAtAll bool, atMobiles []string) {
	url := "https://oapi.dingtalk.com/robot/send?access_token=" + token
	if !isAtAll && len(atMobiles) > 0 {
		atMobileStr := "@" + strings.Join(atMobiles, " @")
		content = content + " \n " + atMobileStr
	}
	params := map[string]any{
		"msgtype": "text",
		"text": map[string]any{
			"content": content,
		},
		"at": map[string]any{
			"isAtAll":   isAtAll,
			"atMobiles": atMobiles,
		},
	}
	paramsJson, err := json.Marshal(params)
	if err != nil {
		slog.With("err", err).Error("dingDingNotify marshal params error")
		return
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(paramsJson))
	if err != nil {
		slog.With("err", err).Error("dingDingNotify send request error")
		return
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.With("err", err).Error("dingDingNotify read response error")
		return
	}
	fmt.Println("ding notify rsp", string(body))
}
