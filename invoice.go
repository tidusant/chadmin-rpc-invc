package main

import (
	"time"

	"github.com/tidusant/c3m-common/c3mcommon"
	"github.com/tidusant/c3m-common/inflect"
	"github.com/tidusant/c3m-common/log"
	"github.com/tidusant/c3m-common/lzjs"
	"github.com/tidusant/c3m-common/mystring"
	rpch "github.com/tidusant/chadmin-repo/cuahang"
	"github.com/tidusant/chadmin-repo/models"

	//	"c3m/common/inflect"
	//	"c3m/log"
	"encoding/base64"
	"encoding/json"
	"flag"
	"net"
	"net/rpc"
	"strconv"
	"strings"
)

const (
	defaultcampaigncode string = "XVsdAZGVmYd"
)

type Arith int

func (t *Arith) Run(data string, result *string) error {
	log.Debugf("Call RPCprod args:" + data)
	*result = ""
	//parse args
	args := strings.Split(data, "|")

	if len(args) < 3 {
		return nil
	}
	var usex models.UserSession
	usex.Session = args[0]
	usex.Action = args[2]
	info := strings.Split(args[1], "[+]")
	usex.UserID = info[0]
	ShopID := info[1]
	usex.Params = ""
	if len(args) > 3 {
		usex.Params = args[3]
	}

	//check shop permission
	shop := rpch.GetShopById(usex.UserID, ShopID)
	if shop.Status == 0 {
		*result = c3mcommon.ReturnJsonMessage("-4", "Shop is disabled.", "", "")
		return nil
	}
	usex.Shop = shop

	if usex.Action == "siv" {
		*result = SaveImport(usex)

	} else if usex.Action == "l" {
		*result = LoadInvoices(usex)
	} else if usex.Action == "riv" {
		*result = RemoveInvc(usex)
	} else { //default
		*result = c3mcommon.ReturnJsonMessage("-5", "Action not found.", "", "")
	}

	return nil
}
func RemoveInvc(usex models.UserSession) string {
	//get invc
	invc := rpch.GetInvcById(usex.Shop.ID.Hex(), usex.Params)

	if invc.ID.Hex() == "" {
		return c3mcommon.ReturnJsonMessage("2", "", "no invoice found", "")
	}

	//decrease stock
	for _, item := range invc.Items {
		prod := rpch.GetProdByCode(usex.Shop.ID.Hex(), item.ProductCode)
		for propi, prop := range prod.Properties {
			if prop.Code == item.PropertyCode {
				prod.Properties[propi].Stock -= item.Stock
				rpch.SaveProd(prod)
				break
			}
		}

	}
	if rpch.RemoveInvcById(usex.Shop.ID.Hex(), usex.Params) {
		return c3mcommon.ReturnJsonMessage("1", "", "success", `"`+usex.Params+`"`)
	}

	return c3mcommon.ReturnJsonMessage("0", "remove invoice fail", "", `"`+usex.Params+`"`)
}
func LoadInvoices(usex models.UserSession) string {
	isImport, _ := strconv.ParseBool(usex.Params)
	invcs := rpch.GetInvoices(usex.Shop.ID.Hex(), isImport)
	if len(invcs) == 0 {
		return c3mcommon.ReturnJsonMessage("2", "", "no invoice found", "")
	}
	info, _ := json.Marshal(invcs)
	return c3mcommon.ReturnJsonMessage("1", "", "success", string(info))
}
func SaveImport(usex models.UserSession) string {
	log.Debugf("param:%s", usex.Params)
	args := strings.Split(usex.Params, ",")
	if len(args) < 5 {
		return c3mcommon.ReturnJsonMessage("-5", "invalid params", "", "")
	}
	curlang := args[2]
	imgs := args[1]
	descbytes, _ := base64.StdEncoding.DecodeString(args[0])
	desc := string(descbytes)
	isImport, _ := strconv.ParseBool(args[3])
	created, _ := time.Parse("2006-01-02", args[4])
	var t time.Time
	if created == t {
		created = time.Now()
	}
	var importitems []models.InvoiceItem
	strbytes, _ := base64.StdEncoding.DecodeString(args[5])

	err := json.Unmarshal(strbytes, &importitems)
	if !c3mcommon.CheckError("create cat parse json", err) {
		return c3mcommon.ReturnJsonMessage("0", "properties parse json fail", "", "")
	}
	//get all product
	prods := rpch.GetAllProds(usex.UserID, usex.Shop.ID.Hex())
	prodcodes := make(map[string]models.Product)
	propcodes := make(map[string]models.ProductProperty)

	for _, prod := range prods {
		prodcodes[prod.Code] = prod
		for _, prop := range prod.Properties {
			propcodes[prop.Code] = prop

		}
	}
	//slug
	//get all slug
	slugs := rpch.GetAllSlug(usex.UserID, usex.Shop.ID.Hex())
	mapslugs := make(map[string]string)
	for i := 0; i < len(slugs); i++ {
		mapslugs[slugs[i]] = slugs[i]
	}
	total := 0
	num := 0
	for _, importitem := range importitems {
		log.Debugf("importitem: %s", importitem)
		if importitem.ProductName == "" {
			continue
		}
		//importitem.ProductName = lzjs.CompressToBase64(importitem.ProductName)
		var saveprod models.Product
		createnewprod := true
		//check prod name
		for _, prod := range prodcodes {
			pname, _ := lzjs.DecompressFromBase64(prod.Langs[curlang].Name)
			if pname == importitem.ProductName {
				saveprod = prod
				createnewprod = false
			}
		}

		if createnewprod {
			//create new prod
			var newlang models.ProductLang
			newlang.Name = lzjs.CompressToBase64(importitem.ProductName)
			saveprod.CatId = "unk"
			saveprod.ShopId = usex.Shop.ID.Hex()
			saveprod.UserId = usex.UserID
			saveprod.Main = false
			//newslug
			tb := importitem.ProductName
			newslug := inflect.Parameterize(string(tb))
			newlang.Slug = newslug
			//check slug duplicate
			i := 1
			for {
				if _, ok := mapslugs[newlang.Slug]; ok {
					newlang.Slug = newslug + strconv.Itoa(i)
					i++
				} else {
					mapslugs[newlang.Slug] = newlang.Slug
					break
				}
			}

			//create code
			for {
				saveprod.Code = mystring.RandString(3)
				if _, ok := prodcodes[saveprod.Code]; !ok {
					prodcodes[saveprod.Code] = saveprod
					break
				}
			}
			//log.Debugf("Langs %v, %v", saveprod.Langs, *saveprod.Langs[curlang])
			saveprod.Langs = make(map[string]*models.ProductLang)
			saveprod.Langs[curlang] = &newlang
		}
		//save prop

		iscreatenewprop := true
		for i, _ := range saveprod.Properties {
			if saveprod.Properties[i].Name == importitem.PropertyName {
				if isImport {
					saveprod.Properties[i].Stock += importitem.Stock
					saveprod.Properties[i].BasePrice = importitem.BasePrice
				} else {
					saveprod.Properties[i].Stock -= importitem.Stock
				}
				saveprod.Langs[curlang].Unit = importitem.Unit
				iscreatenewprop = false
				break
			}
		}
		if iscreatenewprop {

			//create new prop
			var newprop models.ProductProperty
			newprop.Name = importitem.PropertyName
			newprop.BasePrice = importitem.BasePrice
			newprop.Stock = importitem.Stock
			//create new prop code
			for {
				newprop.Code = mystring.RandString(4)
				if _, ok := propcodes[newprop.Code]; !ok {
					propcodes[newprop.Code] = newprop
					break
				}
			}
			saveprod.Langs[curlang].Unit = importitem.Unit
			saveprod.Properties = append(saveprod.Properties, newprop)
		}

		//update db
		log.Debugf("saveprod %v", saveprod)
		rpch.SaveProd(saveprod)
		if saveprod.ID.Hex() == "" {
			saveprod = rpch.GetProdByCode(usex.Shop.ID.Hex(), saveprod.Code)
		}
		prodcodes[saveprod.Code] = saveprod
		total += importitem.Stock * importitem.BasePrice
		num += importitem.Stock

	}
	//save invoice
	var invc models.Invoice
	invc.Items = importitems
	invc.Images = strings.Split(imgs, ",")
	invc.Description = desc
	invc.Total = total
	invc.Num = num
	invc.Import = isImport
	invc.UserId = usex.UserID
	invc.ShopId = usex.Shop.ID.Hex()
	invc.Created = created.Unix()
	invc.Modified = created.Unix()

	invc.Search = inflect.ParameterizeJoin(desc+" "+strconv.Itoa(t.Day())+" "+strconv.Itoa(int(t.Month()))+" "+strconv.Itoa(t.Year()), " ")
	invc = rpch.SaveInvoice(invc)
	info, _ := json.Marshal(invc)

	return c3mcommon.ReturnJsonMessage("1", "", "", string(info))

}

func main() {
	var port int
	var debug bool
	flag.IntVar(&port, "port", 9891, "help message for flagname")
	flag.BoolVar(&debug, "debug", false, "Indicates if debug messages should be printed in log files")
	flag.Parse()

	//logLevel := log.DebugLevel
	if !debug {
		//logLevel = log.InfoLevel

	}

	// log.SetOutputFile(fmt.Sprintf("adminDash-"+strconv.Itoa(port)), logLevel)
	// defer log.CloseOutputFile()
	// log.RedirectStdOut()

	//init db
	arith := new(Arith)
	rpc.Register(arith)
	log.Infof("running with port:" + strconv.Itoa(port))

	//			rpc.HandleHTTP()
	//			l, e := net.Listen("tcp", ":"+strconv.Itoa(port))
	//			if e != nil {
	//				log.Debug("listen error:", e)
	//			}
	//			http.Serve(l, nil)

	tcpAddr, err := net.ResolveTCPAddr("tcp", ":"+strconv.Itoa(port))
	c3mcommon.CheckError("rpc dail:", err)

	listener, err := net.ListenTCP("tcp", tcpAddr)
	c3mcommon.CheckError("rpc init listen", err)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go rpc.ServeConn(conn)
	}
}
