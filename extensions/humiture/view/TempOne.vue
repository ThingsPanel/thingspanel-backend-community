<template>
  <!-- 随机密码生成：https://c.runoob.com/front-end/686/ -->
  <!-- 所有class后必须跟随机密码，打开上面网页勾掉特殊字符生成随机密码如下kA6iA2 -->
  <div class="chart-out-bU2bE4">
    <!-- 总盒子（总盒子样式勿要修改） -->
    <div class="chart-all-bU2bE4">
      <!-- 标题盒子（每个图表必须有标题）格式和样式请勿修改 -->
      <div class="chart-top-bU2bE4">
        <div style="
            text-align:center;
            color: #fff;
            width: 100%;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
          ">
          当前温度
        </div>
      </div>
      <!-- 图表主盒子（主盒子样式勿要修改） -->
      <div class="chart-body-bU2bE4" :id="'chart_' + id"></div>
    </div>
  </div>
</template>
<script>
  //import * as echarts from "echarts";
  export default {
    props: {
      id: {
        type: Number,
        default: 0,
      },
      loading: {
        type: Boolean,
        default: true,
      },
      apiData: {
        type: Object,
      },
      legend: {
        type: Boolean,
        default: true,
      },
    },
    data() {
      return {
        latest: {},
        fields: [],
        chart: null,
        dataOne: 0,
      };
    },
    watch: {
      apiData: {
        //document.getElementById('ElementId').removeAttribute("style")
        //style="width: 100%; height: 100%"
        //apiData的数据样例，fields是传感器一段时间内的数据，latest是传感器最新数据
        // {
        //       "device_id": "f1ab2c47-951f-10b8-60c0-c6b33440f189",
        //       "fields": [
        //           {
        //               "hum": 24,
        //               "systime": "2022-01-18 18:59:11",
        //               "temp": 26
        //           },
        //           {
        //               "hum": 24,
        //               "systime": "2022-01-18 18:59:17",
        //               "temp": 26
        //           },
        //           {
        //               "hum": 24,
        //               "systime": "2022-01-18 18:59:41",
        //               "temp": 26
        //           }
        //       ],
        //       "latest": {
        //           "hum": 24,
        //           "systime": "2022-01-18 18:59:41",
        //           "temp": 26
        //       }
        //   }
        // deep: true,
        immediate: true,
        handler(val, oldVal) {
          console.log("01-bU2bE4-图表接收到数据");
          console.log("02-bU2bE4-图表id:" + this.id);
          if (val["fields"]) {
            console.log("03-bU2bE4-fields有值");
            console.log("04-bU2bE4-device_id:" + val["device_id"]);
            this.latest = val["latest"];
            this.fields = val["fields"];
            this.getData();
          } else {
            console.log("05-bU2bE4-fields没有值");
          }
        },
      },
      colorStart() { },
      colorEnd() { },
      legend(val, oldVal) {
        this.chart.setOption({
          legend: {
            show: val,
          },
        });
      },
    },
    // mounted() {
    //   this.initChart();
    //   const resizeObserver = new ResizeObserver((entries) => {
    //     //回调,重置图表大小
    //     this.chart && this.chart.resize();
    //   });
    //   resizeObserver.observe(document.getElementById("chart_" + this.id));
    // },
    methods: {
      // 给图表中的数据赋值
      getData() {
        this.dataOne = this.latest.temp;
        setTimeout(() => {
          this.initChart();
        }, 1000);
      },
      initChart() {
        console.log("05-bU2bE4-初始化图表开始");
        this.chart = echarts.init(document.getElementById("chart_" + this.id));
        var option = {
          series: [
            {
              type: "gauge",
              min: -20,
              max: 40,
              progress: {
                show: true,
                width: 10,
                // color:'#3ECF63'
              },
              itemStyle: {
                color: "#3ECF63",
              },
              pointer: {
                show: false,
              },
              axisLine: {
                lineStyle: {
                  width: 10,
                },
              },
              axisTick: {
                show: false,
              },
              splitLine: {
                length: 15,
                lineStyle: {
                  width: 0,
                  // color: '#999'
                },
              },
              axisLabel: {
                distance: 25,
                color: "#999",
                fontSize: 0,
              },
              // anchor: {
              //   show: false,
              //   showAbove: false,
              //   size: 25,
              //   itemStyle: {
              //     borderWidth: 10
              //   }
              // },
              title: {
                show: true,
                fontSize: 15,
                color: "#5B92FF",
                offsetCenter: [0, "15%"],
              },
              detail: {
                valueAnimation: true,
                width: "80%",
                lineHeight: 40,
                borderRadius: 8,
                offsetCenter: [0, "-10%"],
                fontSize: 40,
                fontWeight: "bolder",
                formatter: function (value) {
                  return "{value|" + value + "}{unit|℃}";
                },
                rich: {
                  value: {
                    fontSize: 40,
                  },
                  unit: {
                    fontSize: 20,
                  },
                },
                color: "#fff",
              },
              data: [
                {
                  value: this.dataOne,
                },
              ],
            },
          ],
        };
        this.chart.setOption(option);
        console.log("06-bU2bE4-初始化图表完成");
        const resizeObserver = new ResizeObserver((entries) => {
          this.chart && this.chart.resize();
        });
        resizeObserver.observe(document.getElementById("chart_" + this.id));
      },
      /**
       * 重置图表大小
       */
      resizeChart() {
        /* eslint-disable no-unused-expressions */
        this.chart && this.chart.resize();
      },
    },
  };
</script>
<style scoped>
  .chart-out-bU2bE4 {
    width: 100%;
    height: 100%;
    position: relative;
  }

  /* 请勿修改chart-all */
  .chart-all-bU2bE4 {
    width: 100%;
    height: 100%;
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    /* border: 1px solid rgb(41, 189, 139); */
  }

  /* 请勿修改chart-top */
  .chart-top-bU2bE4 {
    padding-left: 0px;
    left: 0px;
    top: 0px;
    width: 100%;
    height: 20px;
    box-sizing: border-box;
    /* border: 2px solid rgb(24, 222, 50); */
  }

  /* 请勿修改chart-body */
  .chart-body-bU2bE4 {
    width: 100%;
    height: calc(100% - 50px);
    /* border: 2px solid rgb(201, 26, 26); */
  }
</style>