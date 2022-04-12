<template>
  <!--插件调试区域01-->
  <div style="width: 100%; height: 100%">
    <div class="chart-top">
      <div style="color: #fff;">空气污染指数</div>
      <div style="color: #5b92ff">Air pollution index</div>
    </div>
    <div class="chart-body" :id="'chart_' + id">
    </div>
    <div class="chart-bottom">
      <div style="color: #fff;">1小时pm2.5的质量为</div>
      <div :style="'color:#fff;border: 0px solid #000;width: 40px;float: right;background-color:'+qColor+';position: relative;'">{{qLevel}}</div>

    </div>
  </div>
</template>
<script>
  // import * as echarts from "echarts";
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
        // 插件调试区域02
        latest: {},
        fields: [],
        airList:[],
        chart: null,
        pm25: 0,
        qColor:"#fff",
        qLevel:"一级"
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
          console.log("---------------------------------接收到apiData");
          console.log("---------------------------------id:" + this.id);
          if (val["fields"]) {
            console.log("---------------------------------fields有值:");
            console.log(
              "---------------------------------device_id:" + val["device_id"]
            );
            this.latest = val["latest"];
            this.fields = val["fields"];
            this.getData();
          } else {
            console.log("---------------------------------fields没有值:");
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
    mounted() {
      this.getQuality(this.pm25)
      
      this.initChart();
      const resizeObserver = new ResizeObserver((entries) => {
        this.chart && this.chart.resize();
      });
      resizeObserver.observe(document.getElementById("chart_" + this.id));
    },
    methods: {
     
      // 插件调试区域03
      getQuality(myValue){
        if (myValue >= 250){         
          this.qColor="#7e0023"
          this.qLevel="六级"
        }else if (myValue >= 200){
          this.qColor="#b32016"
          
          this.qLevel="五级"
        }else if (myValue >= 150){
          
          this.qColor="#ff9933"
          this.qLevel="四级"
        }else if (myValue >= 100){
          this.qColor="#f6dd26"
          
          this.qLevel="三级"
        }else if (myValue >= 50){
          this.qColor="#d0da13"
          this.qColor="#b32016"
          this.qLevel="二级"
        }else if (myValue >= 0){ 
          this.qColor="#088426"       
          
          this.qLevel="一级"
        }

      },
      getData() {
        this.pm25 = this.latest.pm25
        this.initChart();
        this.getQuality(this.pm25)
        
      },
      initChart() {
        console.log("---------------------------------初始化图表开始");
        var chartDom = document.getElementById("chart_" + this.id);
        this.chart = echarts.init(chartDom);
        var option;
        option = {
          series: [
            
            {
              type: 'gauge',
              radius: '90%',
              center: ['50%', '95%'],
              startAngle: 180,
              endAngle: 0,
              min: 0,
              max: 300,
              splitNumber: 12,
              axisLine: {
                lineStyle: {
                  width:230,
                  color: [
                    [0.17, '#088426'],
                    [0.34, '#d0da13'],
                    [0.51, '#f6dd26'],
                    [0.68, '#ff9933'],
                    [0.85, '#b32016'],
                    [1, '#7e0023']
                  ]
                }
              },
              anchor: {
                show: false,
              },
              pointer: {
                show: false,    
              },
              axisTick: {
                show: false,
              },
              splitLine: {
                show: false,
              },
              axisLabel: {
                show: false,
              },
              title: {
                show: false,
              },
              detail: {
                show: false,
              },             
            },
            {
              type: 'gauge',
              radius: '90%',
              center: ['50%', '95%'],
              startAngle: 180,
              endAngle: 0,
              min: 0,
              max: 300,
              splitNumber: 12,
              axisLine: {
                lineStyle: {
                  width: 20,
                  color: [
                    [0.17, '#fff'],
                    [0.34, '#fff'],
                    [0.51, '#fff'],
                    [0.68, '#fff'],
                    [0.85, '#fff'],
                    [1, '#fff']
                  ]
                }
              },
              pointer: {
                show: false,             
              },
              axisTick: {
                show: false,               
              },
              splitLine: {
                show: false,
                length: 20,
                lineStyle: {
                  color: 'auto',
                  width: 5
                }
              },
              axisLabel: {
                show: true,
                color: '#12357B',
                fontSize: 12,
                distance: -25,
                formatter: function (value) {
                  if (value === 300) {
                    return '';
                  } else if (value === 275) {
                    return '严重';
                  } else if (value === 250) {
                    return '';
                  } else if (value === 225) {
                    return '重度';
                  } else if (value === 200) {
                    return '';
                  } else if (value === 175) {
                    return '中度';
                  } else if (value === 150) {
                    return '';
                  } else if (value === 125) {
                    return '轻度';
                  } else if (value === 100) {
                    return '';
                  } else if (value === 75) {
                    return '良';
                  } else if (value === 50) {
                    return '';
                  } else if (value === 25) {
                    return '优';
                  } else if (value === 0) {
                    return '';
                  }
                  return '';
                }
              },
              title: {
                show: false,  
              },
              detail: {
                show: false,   
              },
            },
            {
              type: 'gauge',
              radius: '25%',
              center: ['50%', '95%'],
              startAngle: 180,
              endAngle: 0,
              min: 0,
              max: 300,
              // splitNumber: 6,
              axisLine: {
                lineStyle: {
                  width:50,
                  color: [
                    [0.17, '#fff'],
                    [0.34, '#fff'],
                    [0.51, '#fff'],
                    [0.68, '#fff'],
                    [0.85, '#fff'],
                    [1, '#fff']
                  ]
                }
              },
              pointer: {
                show: false,
              },
              axisTick: {
                show: false,
              },
              splitLine: {
                show: false,
              },
              axisLabel: {
                show: false,
              },
              title: {
                show: false,
              },
              detail: {
                show: false,
              },
              
            },
            {
              type: 'gauge',
              radius: '15%',
              center: ['50%', '95%'],
              startAngle: 180,
              endAngle: 0,
              min: 0,
              max: 300,
              // splitNumber: 6,
              axisLine: {
                lineStyle: {
                  width: 50,
                  color: [
                    [0.17, '#eee'],
                    [0.34, '#eee'],
                    [0.51, '#eee'],
                    [0.68, '#eee'],
                    [0.85, '#eee'],
                    [1, '#eee']
                  ]
                }
              },

              anchor: {
                show: true,
                showAbove: true,
                size: 10,
                icon: 'circle',
                offsetCenter: [0, '0%'],
                keepAspect: false,
                itemStyle: {
                  borderWidth: 2,
                  borderColor: '#eee'
                }

              },
              pointer: {
                width: 10,
                length: '350%',
                offsetCenter: ['0%', '0%'],
                itemStyle: {
                  borderColor: '#fff',
                  borderWidth: 2,
                  borderCap: 'round',
                  color: '#fff',
                  opacity: 0.7,
                  shadowColor: 'rgba(0, 0, 0, 0.5)',
                  shadowBlur: 10
                },
                icon: 'triangle'
              },

              axisTick: {
                show: false,
              },
              splitLine: {
                show: false,
              },
              axisLabel: {
                show: false,
              },
              title: {
                show: false,
              },
              detail: {
                show: true,
                fontSize: 50,
                color: '#fff',
                offsetCenter: ['-500%', '-690%'],
              },
              data: [
                {
                  value: this.pm25,
                }
              ]
            },
            
          ]
        };
        //this.chart.clear();
        this.chart.setOption(option);
        
        console.log("---------------------------------初始化图表完成");
      },
    },
  };
</script>
<!--插件调试区域04-->
<style scoped>
  .chart-top {
    width: 100%;
    height: 10%;
    /* position: absolute;
    top: 46px;
    left: 22px; */
    /* border: 1px solid rgb(187, 46, 46); */
    
  }

  .chart-body {
    /* margin: 5%; */
    max-width: 100%;
    max-height: 80%;
    position: absolute;
    top: 0;
    left: 0;
    bottom: 0;
    right: 0;
    margin: auto;
    /* width: 100%;
    height: 90%; */
    /* border: 1px solid rgb(23, 173, 60); */

  }

  .chart-bottom {
    text-align: right;
    position: relative;
    top: 20%;
    width: 85%;
    height: 10%;
    margin: 0 auto;
    /* border: 1px solid rgb(155, 211, 25); */
  }

  .chart-img {
    max-width: 60%;
    max-height: 60%;
    /* width: 100%; */
    position: absolute;
    top: 0;
    left: 0;
    bottom: 0;
    right: 0;
    margin: auto;
    /* padding-bottom: 9%; */

    /* margin-top: -165px; */
    /* border: 1px solid rgb(187, 46, 46); */


  }
</style>